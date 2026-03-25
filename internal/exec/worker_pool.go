package exec

import (
	"context"
	"sync"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type WorkerPool struct {
	workers int
	sem     chan struct{}
	wg      sync.WaitGroup
}

func NewWorkerPool(workers int) *WorkerPool {
	if workers <= 0 {
		workers = 3
	}
	return &WorkerPool{
		workers: workers,
		sem:     make(chan struct{}, workers),
	}
}

func (wp *WorkerPool) ExecuteToolCalls(ctx context.Context, calls []ToolCallInput, executor func(context.Context, ToolCallInput) sdk.ToolResult) []sdk.ToolResult {
	results := make([]sdk.ToolResult, len(calls))
	if len(calls) == 0 {
		return results
	}

	if len(calls) == 1 || wp.workers == 1 {
		for i, call := range calls {
			results[i] = executor(ctx, call)
		}
		return results
	}

	type result struct {
		index  int
		result sdk.ToolResult
	}

	resultChan := make(chan result, len(calls))

	for i, call := range calls {
		wp.wg.Add(1)
		go func(idx int, c ToolCallInput) {
			defer wp.wg.Done()
			wp.sem <- struct{}{}
			defer func() { <-wp.sem }()

			select {
			case <-ctx.Done():
				resultChan <- result{index: idx, result: sdk.ToolResult{StepID: c.ID, Success: false, Error: "context cancelled"}}
			default:
				resultChan <- result{index: idx, result: executor(ctx, c)}
			}
		}(i, call)
	}

	go func() {
		wp.wg.Wait()
		close(resultChan)
	}()

	for r := range resultChan {
		results[r.index] = r.result
	}

	return results
}

func (wp *WorkerPool) Close() {
	wp.wg.Wait()
}

type ToolCallInput struct {
	ID        string
	Name      string
	Arguments map[string]any
}

var DefaultWorkerPool = NewWorkerPool(3)
