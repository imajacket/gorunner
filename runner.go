package gorunner

import (
	"errors"
)

// Runner manages named InstructionSet(s) and invokes them concurrently up to a specified limit.
//
// A Runner is configured with a retry count and a maximum concurrency level.
// Each named InstructionSet can be invoked by calling Run. Type-checking is performed
// at runtime to ensure that the provided arguments match the expected type of the
// InstructionSet.
type Runner struct {
	// sets is an internal map of InstructionSet(s) keyed by name.
	sets map[string]instructionSet
	// semaphore is a buffered channel used to enforce concurrency limits.
	semaphore chan struct{}
	// retries is the number of times an InstructionSet will be retried on error.
	retries int
}

// New creates and returns a new Runner.
//
// The 'retries' parameter specifies how many times an InstructionSet will be retried upon error.
// The 'concurrency' parameter specifies the maximum number of concurrent InstructionSet
// invocations allowed.
//
// Example usage:
//
//	runner := gorunner.New(10, 10)
//	set := gorunner.NewInstructionSet[int]("count", nil, nil)
//	set.Save(runner)
//	err := runner.Run("count", 42)
func New(retries, concurrency int) *Runner {
	return &Runner{
		sets:      make(map[string]instructionSet),
		retries:   retries,
		semaphore: make(chan struct{}, concurrency),
	}
}

// RemoveSet removes the InstructionSet associated with the given name from the Runner.
//
// If no InstructionSet is found for the provided name, this function does nothing.
func (r *Runner) RemoveSet(name string) {
	delete(r.sets, name)
}

// Run invokes the named InstructionSet with the provided arguments. If the InstructionSet
// does not exist, an error is returned immediately. If the argument type does not match
// what the InstructionSet expects, an error is also returned immediately.
//
// Run returns an error only if the InstructionSet is not found or if the argument type is invalid.
// It does not wait for the instructions to finish. If the InstructionSet exists and the type
// check passes, Run starts the execution asynchronously and returns nil.
//
// Concurrency is limited by the Runner’s internal semaphore. If concurrency has reached
// its limit, the call to Run will block until a "slot" is available.
func (r *Runner) Run(name string, args any) error {
	set, ok := r.sets[name]
	if !ok {
		return errors.New("instructions not found")
	}

	// Type check
	err := set.check(args)
	if err != nil {
		return err
	}

	r.semaphore <- struct{}{}

	go func() {
		defer func() {
			<-r.semaphore
		}()
		set.run(args, r.retries)
	}()

	return nil
}
