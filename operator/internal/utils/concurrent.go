// /*
// Copyright 2025 The Grove Authors.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */

package utils

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/go-logr/logr"
	"github.com/samber/lo"
)

// Task represents a named function to be executed concurrently.
type Task struct {
	// Name identifies the task for logging and reporting.
	Name string
	// Fn is the function to execute with context.
	Fn func(ctx context.Context) error
}

// RunResult tracks the execution status of concurrent tasks.
type RunResult struct {
	// SuccessfulTasks contains names of successfully executed tasks
	SuccessfulTasks []string
	// FailedTasks contains names of tasks that encountered errors
	FailedTasks []string
	// SkippedTasks contains names of tasks that were not executed
	SkippedTasks []string
	// Errors contains all errors encountered during execution
	Errors []error
}

// HasErrors returns true if any tasks failed during execution.
func (r *RunResult) HasErrors() bool {
	return len(r.Errors) > 0
}

// GetAggregatedError joins all task errors into a single error.
func (r *RunResult) GetAggregatedError() error {
	if !r.HasErrors() {
		return nil
	}
	return errors.Join(r.Errors...)
}

// GetSummary returns a string representation of task execution results.
func (r *RunResult) GetSummary() string {
	return fmt.Sprintf("RunResult{SuccessfulTasks: %v, FailedTasks: %v, SkippedTasks: %v}",
		r.SuccessfulTasks, r.FailedTasks, r.SkippedTasks)
}

// RunConcurrentlyWithSlowStart executes tasks in exponentially growing batches.
// It starts with initialBatchSize and doubles batch size on success. On any error,
// execution halts immediately. This prevents overwhelming kube-apiserver with
// too many concurrent requests by gradually increasing load.
func RunConcurrentlyWithSlowStart(ctx context.Context, logger logr.Logger, initialBatchSize int, tasks []Task) RunResult {
	remaining := len(tasks)
	aggregatedRunResult := RunResult{}
	nextRunStartIndex := 0
	for batchSize := min(remaining, initialBatchSize); batchSize > 0; batchSize = min(2*batchSize, remaining) {
		logger.V(4).Info("Triggering batch of taskConfigs with slow start", "batchSize", batchSize, "remainingTasks", remaining)
		runEndIndex := nextRunStartIndex + batchSize
		batchRunResult := RunConcurrently(ctx, logger, tasks[nextRunStartIndex:runEndIndex])
		updateWithBatchRunResult(&aggregatedRunResult, batchRunResult)
		if batchRunResult.HasErrors() {
			logger.V(4).Info("Batch of taskConfigs failed, halting further execution", "batchSize", batchSize, "remainingTasks", remaining)
			computeAndUpdateSkippedTasks(&aggregatedRunResult, tasks)
			return aggregatedRunResult
		}
		remaining -= batchSize
		nextRunStartIndex = runEndIndex
	}
	return aggregatedRunResult
}

// RunConcurrently executes all tasks concurrently without bounds.
func RunConcurrently(ctx context.Context, logger logr.Logger, tasks []Task) RunResult {
	return RunConcurrentlyWithBounds(ctx, logger, tasks, len(tasks))
}

// RunConcurrentlyWithBounds executes tasks with a maximum of bound concurrent executions.
func RunConcurrentlyWithBounds(ctx context.Context, logger logr.Logger, tasks []Task, bound int) RunResult {
	rg := newRunGroup(bound, logger)
	for _, task := range tasks {
		rg.trigger(ctx, task)
	}
	tasksInError := rg.waitAndCollectErroneousTasks()
	return createRunResult(tasks, tasksInError)
}

// RunResult helper functions
// -------------------------------------------------------------------------------------------
func createRunResult(allTasks []Task, tasksInError []lo.Tuple2[string, error]) RunResult {
	result := RunResult{
		SuccessfulTasks: make([]string, 0, len(allTasks)),
		FailedTasks:     make([]string, 0, len(tasksInError)),
		SkippedTasks:    make([]string, 0, len(allTasks)-len(tasksInError)),
		Errors:          make([]error, 0, len(tasksInError)),
	}
	for _, task := range allTasks {
		foundErrTask, ok := lo.Find(tasksInError, func(errTask lo.Tuple2[string, error]) bool {
			return errTask.A == task.Name
		})
		if ok {
			result.FailedTasks = append(result.FailedTasks, foundErrTask.A)
			result.Errors = append(result.Errors, foundErrTask.B)
		} else {
			result.SuccessfulTasks = append(result.SuccessfulTasks, task.Name)
		}
	}
	return result
}

func updateWithBatchRunResult(aggregatedRunResult *RunResult, batchRunResult RunResult) {
	aggregatedRunResult.SuccessfulTasks = append(aggregatedRunResult.SuccessfulTasks, batchRunResult.SuccessfulTasks...)
	aggregatedRunResult.FailedTasks = append(aggregatedRunResult.FailedTasks, batchRunResult.FailedTasks...)
	aggregatedRunResult.Errors = append(aggregatedRunResult.Errors, batchRunResult.Errors...)
}

func computeAndUpdateSkippedTasks(result *RunResult, allTasks []Task) {
	allTaskNames := lo.Map(allTasks, func(task Task, _ int) string {
		return task.Name
	})
	skippedTaskNames := lo.Filter(allTaskNames, func(taskName string, _ int) bool {
		return !lo.Contains(result.SuccessfulTasks, taskName) && !lo.Contains(result.FailedTasks, taskName)
	})
	result.SkippedTasks = append(result.SkippedTasks, skippedTaskNames...)
}

// Types and functions/methods to manage concurrent execution of taskConfigs
// -------------------------------------------------------------------------------------------

// runGroup coordinates concurrent task execution and error collection.
type runGroup struct {
	logger    logr.Logger
	wg        sync.WaitGroup
	errTaskCh chan lo.Tuple2[string, error]
}

// newRunGroup creates a new runGroup with buffered error channel.
func newRunGroup(numTasks int, logger logr.Logger) *runGroup {
	return &runGroup{
		logger:    logger,
		wg:        sync.WaitGroup{},
		errTaskCh: make(chan lo.Tuple2[string, error], numTasks),
	}
}

// trigger starts asynchronous execution of a task with panic recovery.
func (rg *runGroup) trigger(ctx context.Context, task Task) {
	rg.wg.Add(1)
	rg.logger.V(4).Info("triggering concurrent execution of task", "taskName", task.Name)
	go func(task Task) {
		defer rg.wg.Done()
		defer func() {
			if v := recover(); v != nil {
				stack := debug.Stack()
				panicErr := fmt.Errorf("task: %s execution panicked: %v\n, stack-trace: %s", task.Name, v, stack)
				rg.errTaskCh <- lo.T2(task.Name, panicErr)
			}
		}()
		rg.logger.V(5).Info("executing task", "taskName", task.Name)
		if err := task.Fn(ctx); err != nil {
			rg.errTaskCh <- lo.T2(task.Name, err)
		}
	}(task)
}

// waitAndCollectErroneousTasks waits for all tasks to complete and returns errors.
func (rg *runGroup) waitAndCollectErroneousTasks() []lo.Tuple2[string, error] {
	rg.wg.Wait()
	close(rg.errTaskCh)
	var tasksInError []lo.Tuple2[string, error]
	for err := range rg.errTaskCh {
		tasksInError = append(tasksInError, err)
	}
	return tasksInError
}
