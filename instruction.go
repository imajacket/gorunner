package gorunner

import (
	"errors"
	"fmt"
	"log"
	"reflect"
)

// instructionSet is an internal interface that allows Runner to manage
// different typed InstructionSet(s) without duplication.
type instructionSet interface {
	// run executes the InstructionSet with the given data and retry limit.
	run(data any, retries int)
	// check validates that the provided data matches the InstructionSet’s expected type.
	check(data any) error
}

// Instruction represents a single function in an InstructionSet pipeline.
// It receives a value of type T and returns a transformed value of type T
// and an error if something goes wrong.
type Instruction[T any] func(shared T) (T, error)

// InstructionSet holds a series of Instruction[T], an optional onErr handler,
// an optional onSuccess handler, and a flag to indicate whether the original
// input data should be provided to onErr. It uses a builder-like pattern
// for configuration.
type InstructionSet[T any] struct {
	onErr        func(error, ...T)
	onSuccess    func(T)
	name         string
	instructions []Instruction[T]
	provideToErr bool
}

// NewInstructionSet creates a new typed InstructionSet, identified by a unique name.
//
// The onErr callback is optional. When invoked, it will receive the final error,
// and if provideToErr is set, also the original input data.
//
// The onSuccess callback is optional. It will be invoked with the final
// successfully processed data after all Instructions complete without error.
//
// Example usage:
//
//	runner := gorunner.New(10, 10)
//	set := gorunner.NewInstructionSet[int]("count",
//	    func(err error, data ...int) { fmt.Println("Error:", err, data) },
//	    func(result int) { fmt.Println("Success:", result) },
//	)
//
//	set.Add(func(data int) (int, error) { return data + 1, nil }).
//	    Add(func(data int) (int, error) { return data + 3, nil }).
//	    IncludeDataWithError().
//	    Save(runner)
//
//	err := runner.Run("count", 42)
func NewInstructionSet[T any](
	name string,
	onErr func(error, ...T),
	onSuccess func(T),
) *InstructionSet[T] {
	return &InstructionSet[T]{
		name:         name,
		instructions: make([]Instruction[T], 0),
		onErr:        onErr,
		onSuccess:    onSuccess,
	}
}

// IncludeDataWithError is an optional configuration for an InstructionSet.
// When set, it ensures that the onErr callback receives the original arguments
// provided to Run alongside the error.
func (i *InstructionSet[T]) IncludeDataWithError() *InstructionSet[T] {
	i.provideToErr = true
	return i
}

// Add appends a new Instruction[T] to the InstructionSet. Instructions will be
// executed in the order they are added when the InstructionSet is invoked.
func (i *InstructionSet[T]) Add(instruction Instruction[T]) *InstructionSet[T] {
	i.instructions = append(i.instructions, instruction)
	return i
}

// Save registers the current InstructionSet with the given Runner. If an InstructionSet
// with the same name already exists in the Runner, an error is returned. Otherwise,
// it is stored and can be invoked by calling runner.Run(i.name, ...).
func (i *InstructionSet[T]) Save(runner *Runner) error {
	_, ok := runner.sets[i.name]
	if !ok {
		runner.sets[i.name] = i
		return nil
	}

	return errors.New("instruction already exists")
}

// check first verifies that the InstructionSet being has Instruction(s)
// and returns an error if it does not.
//
// check then verifies that the provided args match the type parameter T of this InstructionSet.
// If the types do not match, check returns an error describing the mismatch.
func (i *InstructionSet[T]) check(args any) error {
	if len(i.instructions) == 0 {
		return errors.New("no instructions")
	}

	_, valid := args.(T)
	if !valid {
		argsType := reflect.TypeOf(args)
		validType := reflect.TypeOf((*T)(nil)).Elem()

		return fmt.Errorf(
			"expected argument of type %s for instruction set '%s', got %s",
			validType.Name(),
			i.name,
			argsType.Name(),
		)
	}

	return nil
}

// runSet executes the sequence of Instructions in order, passing the transformed
// result from each Instruction to the next. If any Instruction returns an error,
// runSet immediately returns the last successful result (up to that point) along
// with the error.
func (i *InstructionSet[T]) runSet(args T) (T, error) {
	var results T

	results, err := i.instructions[0](args)
	if err != nil {
		return results, err
	}

	for index, instruction := range i.instructions {
		if index == 0 {
			continue
		}

		r, rErr := instruction(results)
		if rErr != nil {
			return results, rErr
		}

		results = r
	}
	return results, nil
}

// run handles the retry logic and the onErr/onSuccess callbacks. It first casts
// the provided args to T. Then, it attempts to run the entire InstructionSet
// up to 'retries' times if an error is encountered.
//
// On the first successful execution, the onSuccess callback (if any) is invoked
// and run returns. If all attempts fail, the onErr callback (if any) is invoked.
func (i *InstructionSet[T]) run(args any, retries int) {
	actualArgs := args.(T)
	var finalErr error
	for index := range retries {
		results, iErr := i.runSet(actualArgs)
		if iErr == nil {
			if i.onSuccess != nil {
				i.onSuccess(results)
			}
			return
		}

		finalErr = iErr
		if index != retries-1 {
			log.Println(fmt.Sprintf("Attempting retry for %s", i.name))
		}
	}

	// If we're here, all attempts failed.
	if i.onErr == nil {
		return
	}

	if !i.provideToErr {
		i.onErr(finalErr)
		return
	}

	i.onErr(finalErr, actualArgs)
}
