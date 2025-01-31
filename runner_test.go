package gorunner

import (
	"errors"
	"log"
	"sync"
	"testing"
	"time"
)

// TestBasicSuccess checks that a single InstructionSet with one instruction
// runs successfully and calls onSuccess with the correct final value.
func TestBasicSuccess(t *testing.T) {
	var (
		successCalled bool
		successValue  int
	)

	// Create an InstructionSet for int
	set := NewInstructionSet[int](
		"test_success",
		nil, // onErr not needed for this test
		func(result int) {
			successCalled = true
			successValue = result
		},
	)

	// Add a simple instruction
	set.Add(func(data int) (int, error) {
		return data + 1, nil
	})

	runner := New(1, 1) // retries=1, concurrency=1
	err := set.Save(runner)
	if err != nil {
		t.Fatalf("Failed to save instruction set: %v", err)
	}

	// Run it
	runErr := runner.Run("test_success", 10)
	if runErr != nil {
		t.Fatalf("Unexpected error running instruction set: %v", runErr)
	}

	// Give goroutine time to complete
	time.Sleep(100 * time.Millisecond)

	if !successCalled {
		t.Errorf("Expected onSuccess to be called")
	}
	if successValue != 11 {
		t.Errorf("Expected final result 11, got %d", successValue)
	}
}

// TestErrorAndRetry checks that if an instruction fails, we retry up to the specified limit.
// Once we hit a success, we stop retrying. If all fail, onErr should be called.
func TestErrorAndRetry(t *testing.T) {
	var (
		successCount int
		errorCount   int
	)

	// This instruction fails for the first 2 attempts, then succeeds.
	faultyInstruction := func(attemptsBeforeSuccess int) Instruction[int] {
		var attempt int
		return func(data int) (int, error) {
			attempt++
			if attempt <= attemptsBeforeSuccess {
				return 0, errors.New("failed attempt")
			}
			return data + 100, nil
		}
	}

	set := NewInstructionSet[int](
		"test_retry",
		func(err error, data ...int) {
			errorCount++
		},
		func(result int) {
			successCount++
		},
	)

	set.Add(faultyInstruction(2))

	// We'll allow 3 total attempts
	runner := New(3, 1)

	err := set.Save(runner)
	if err != nil {
		t.Fatalf("Failed to save instruction set: %v", err)
	}

	// Run with initial value 1
	runErr := runner.Run("test_retry", 1)
	if runErr != nil {
		t.Fatalf("Run should not return an immediate error; got %v", runErr)
	}

	// Wait for goroutines
	time.Sleep(300 * time.Millisecond)

	if successCount != 1 {
		t.Errorf("Expected 1 success call, got %d", successCount)
	}
	if errorCount != 0 {
		t.Errorf("Expected 0 error calls (since eventually succeeded), got %d", errorCount)
	}
}

// TestAllRetriesFail checks that if the instruction keeps failing, onErr is finally called.
func TestAllRetriesFail(t *testing.T) {
	var (
		successCount int
		errorCalled  bool
	)

	// This instruction always fails
	faultyInstruction := func(data int) (int, error) {
		return 0, errors.New("always fails")
	}

	set := NewInstructionSet[int](
		"test_all_fail",
		func(err error, data ...int) {
			errorCalled = true
		},
		func(result int) {
			successCount++
		},
	).IncludeDataWithError()

	set.Add(faultyInstruction)

	// We'll allow 2 total attempts
	runner := New(2, 1)

	err := set.Save(runner)
	if err != nil {
		t.Fatalf("Failed to save instruction set: %v", err)
	}

	runErr := runner.Run("test_all_fail", 5)
	if runErr != nil {
		t.Fatalf("Run should not fail immediately; got %v", runErr)
	}

	// Wait for goroutine
	time.Sleep(300 * time.Millisecond)

	if successCount != 0 {
		t.Errorf("Expected 0 success calls, got %d", successCount)
	}
	if !errorCalled {
		t.Error("Expected onErr to be called after all retries failed")
	}
}

// TestTypeMismatch verifies that an error is returned if the type of argument
// passed to Run doesn't match what the InstructionSet expects.
func TestTypeMismatch(t *testing.T) {
	set := NewInstructionSet[int](
		"test_type_mismatch",
		nil,
		nil,
	).Add(func(data int) (int, error) {
		return data + 1, nil
	})

	runner := New(1, 1)
	_ = set.Save(runner)

	// Passing a string instead of int
	err := runner.Run("test_type_mismatch", "a string")
	if err == nil {
		t.Error("Expected error for type mismatch, but got nil")
	} else {
		expected := "expected argument of type int"
		if err.Error() == "" || len(err.Error()) < len(expected) {
			t.Errorf("Error text should include type mismatch details, got: %v", err)
		}
	}
}

// TestSaveDuplicateName verifies that we cannot save two InstructionSets with the same name.
func TestSaveDuplicateName(t *testing.T) {
	runner := New(1, 1)

	set1 := NewInstructionSet[int]("duplicate_name", nil, nil)
	set2 := NewInstructionSet[int]("duplicate_name", nil, nil)

	if err := set1.Save(runner); err != nil {
		t.Fatalf("Unexpected error saving set1: %v", err)
	}

	if err := set2.Save(runner); err == nil {
		t.Error("Expected error saving duplicate instruction name, got nil")
	}
}

// TestRemoveSet verifies that removing a saved InstructionSet prevents future runs.
func TestRemoveSet(t *testing.T) {
	runner := New(1, 1)

	set := NewInstructionSet[int]("to_remove", nil, nil)
	err := set.Save(runner)
	if err != nil {
		t.Fatalf("Failed to save instruction set: %v", err)
	}

	// Remove the set
	runner.RemoveSet("to_remove")

	// Attempting to Run should fail
	if runErr := runner.Run("to_remove", 10); runErr == nil {
		t.Error("Expected error after removing the set, but got nil")
	}
}

// TestConcurrency verifies that the Runner blocks when concurrency is exceeded.
func TestConcurrency(t *testing.T) {
	// In this test, concurrency=1, so we can’t run two instructions simultaneously.
	runner := New(1, 1)

	var (
		wg       sync.WaitGroup
		starting = make(chan struct{})
	)

	// A slow instruction that holds onto the semaphore
	slowInstruction := func(data int) (int, error) {
		<-starting // wait until main test signals to hold lock
		time.Sleep(200 * time.Millisecond)
		return data, nil
	}

	set1 := NewInstructionSet[int]("concurrency_1", nil, nil)
	set1.Add(slowInstruction)
	_ = set1.Save(runner)

	set2 := NewInstructionSet[int]("concurrency_2", nil, nil)
	set2.Add(func(data int) (int, error) {
		return data + 1, nil
	})
	_ = set2.Save(runner)

	// Run the first set; it will grab the semaphore
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = runner.Run("concurrency_1", 10)
	}()

	// Give a tiny moment to ensure concurrency_1 started
	time.Sleep(50 * time.Millisecond)

	// Start the second set, which should block until set1 finishes
	wg.Add(1)
	go func() {
		defer wg.Done()
		start := time.Now()
		_ = runner.Run("concurrency_2", 100)
		elapsed := time.Since(start)

		// If concurrency is enforced, we expect a noticeable delay
		if elapsed < 150*time.Millisecond {
			t.Errorf("Expected concurrency block; got too little elapsed time: %v", elapsed)
		}
	}()

	close(starting) // Let the first instruction proceed
	wg.Wait()
}

// TestMultipleInstructions verifies that all instructions in the set are executed in order.
func TestMultipleInstructions(t *testing.T) {
	var finalResult int

	set := NewInstructionSet[int](
		"multi_instructions",
		nil,
		func(result int) {
			finalResult = result
		},
	)

	// Chain a few transformations
	set.Add(func(data int) (int, error) {
		return data + 1, nil
	}).Add(func(data int) (int, error) {
		return data * 2, nil
	}).Add(func(data int) (int, error) {
		return data - 5, nil
	})

	runner := New(1, 1)
	_ = set.Save(runner)

	_ = runner.Run("multi_instructions", 10)

	// Wait for result
	time.Sleep(100 * time.Millisecond)
	// (10 + 1) = 11, (11 * 2) = 22, (22 - 5) = 17
	if finalResult != 17 {
		t.Errorf("Expected 17, got %d", finalResult)
	}
}

// Demonstrates usage of logging in your instructions or on-error callback.
func TestInstructionWithLogging(t *testing.T) {
	var (
		errCalled     bool
		successCalled bool
	)

	set := NewInstructionSet[int](
		"log_test",
		func(err error, data ...int) {
			errCalled = true
			log.Printf("Error: %v, original data: %v", err, data)
		},
		func(result int) {
			successCalled = true
			log.Printf("Success with result: %d", result)
		},
	)

	set.Add(func(data int) (int, error) {
		log.Println("Starting instruction...")
		return data + 5, nil
	})

	runner := New(1, 1)
	_ = set.Save(runner)

	_ = runner.Run("log_test", 10)
	time.Sleep(100 * time.Millisecond)

	if !successCalled {
		t.Errorf("Expected success to be called")
	}
	if errCalled {
		t.Errorf("Expected no error callback to be triggered")
	}
}

// TestPartialFailureInChain simulates a chain of instructions where
// a middle instruction fails (once) and triggers a retry.
func TestPartialFailureInChain(t *testing.T) {
	var (
		errCount     int
		successCount int
	)

	// This instruction fails the first time it's called, but succeeds on subsequent attempts.
	var failOnce = true
	failMiddleInstruction := func(data int) (int, error) {
		if failOnce {
			failOnce = false
			return 0, errors.New("middle instruction failed")
		}
		return data + 100, nil
	}

	set := NewInstructionSet[int](
		"partial_fail",
		func(err error, data ...int) {
			errCount++
		},
		func(result int) {
			successCount++
		},
	)

	// 1) Add 10
	// 2) Potentially fail once
	// 3) Add 2
	set.Add(func(data int) (int, error) {
		return data + 10, nil
	}).Add(failMiddleInstruction).
		Add(func(data int) (int, error) {
			return data + 2, nil
		})

	runner := New(2, 1) // 2 retries
	_ = set.Save(runner)

	_ = runner.Run("partial_fail", 1)
	time.Sleep(300 * time.Millisecond)

	// First attempt fails at the second instruction, we have 2 total attempts:
	// - Attempt #1: data => 11, fails => "middle instruction failed"
	// - Attempt #2: data => 11, passes => 111, last instruction => 113
	if successCount != 1 {
		t.Errorf("Expected successCount to be 1, got %d", successCount)
	}
	if errCount != 0 {
		t.Errorf("Expected errCount to be 0 (because we eventually succeeded), got %d", errCount)
	}
}

func TestOnErrWithOriginalData(t *testing.T) {
	var (
		calledWithData bool
		receivedData   int
	)

	set := NewInstructionSet[int](
		"with_data_err",
		func(err error, data ...int) {
			calledWithData = true
			if len(data) > 0 {
				receivedData = data[0]
			}
		},
		nil,
	).IncludeDataWithError()

	set.Add(func(data int) (int, error) {
		return 0, errors.New("fail to test onErr with data")
	})

	runner := New(1, 1)
	_ = set.Save(runner)

	_ = runner.Run("with_data_err", 42)
	time.Sleep(100 * time.Millisecond)

	if !calledWithData {
		t.Error("Expected onErr to be called with data")
	} else if receivedData != 42 {
		t.Errorf("Expected error callback data to be 42, got %d", receivedData)
	}
}

func TestRunnerRunInstructionsNotFound(t *testing.T) {
	runner := New(1, 1)
	if err := runner.Run("missing_instructions", 10); err == nil {
		t.Error("Expected error for missing instructions, got nil")
	}
}

func TestEmptyInstructionSet(t *testing.T) {
	var (
		successCalled bool
		errorCalled   bool
	)
	set := NewInstructionSet[int](
		"empty_set",
		func(err error, data ...int) {
			errorCalled = true
		},
		func(result int) {
			successCalled = true
		},
	)
	// No instructions added

	runner := New(1, 1)
	_ = set.Save(runner)

	// Running an empty instruction set: the code in runSet tries
	// to do instructions[0], which would panic or error out in a real scenario.
	// If you need to handle an empty set gracefully, you’d adjust the code; for now
	// we just test the current behavior.
	if err := runner.Run("empty_set", 10); err == nil {
		t.Error("Expected an out-of-bounds or logic error for empty instructions; got nil")
	}

	time.Sleep(100 * time.Millisecond)

	if successCalled {
		t.Error("Should not have called success for empty instruction set.")
	}
	if errorCalled {
		t.Error("Should not have triggered onErr with this logic, but it depends on your code path.")
	}
}
