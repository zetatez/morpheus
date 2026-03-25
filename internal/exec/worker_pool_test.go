package exec

import (
	"context"
	"testing"
	"time"

	"github.com/zetatez/morpheus/pkg/sdk"
)

func TestNewWorkerPool(t *testing.T) {
	wp := NewWorkerPool(5)
	if wp == nil {
		t.Fatal("NewWorkerPool returned nil")
	}
	if wp.workers != 5 {
		t.Errorf("wp.workers = %d, want 5", wp.workers)
	}
}

func TestNewWorkerPoolDefault(t *testing.T) {
	wp := NewWorkerPool(0)
	if wp.workers != 3 {
		t.Errorf("wp.workers = %d, want default 3", wp.workers)
	}

	wp2 := NewWorkerPool(-1)
	if wp2.workers != 3 {
		t.Errorf("wp2.workers = %d, want default 3", wp2.workers)
	}
}

func TestWorkerPoolExecuteToolCallsEmpty(t *testing.T) {
	wp := NewWorkerPool(3)

	ctx := context.Background()
	executor := func(ctx context.Context, call ToolCallInput) sdk.ToolResult {
		return sdk.ToolResult{Success: true}
	}

	results := wp.ExecuteToolCalls(ctx, []ToolCallInput{}, executor)
	if len(results) != 0 {
		t.Errorf("len(results) = %d, want 0", len(results))
	}
}

func TestWorkerPoolExecuteToolCallsSingle(t *testing.T) {
	wp := NewWorkerPool(3)

	ctx := context.Background()
	executor := func(ctx context.Context, call ToolCallInput) sdk.ToolResult {
		return sdk.ToolResult{Success: true, Data: map[string]any{"id": call.ID}}
	}

	calls := []ToolCallInput{{ID: "call1", Name: "test", Arguments: nil}}
	results := wp.ExecuteToolCalls(ctx, calls, executor)

	if len(results) != 1 {
		t.Fatalf("len(results) = %d, want 1", len(results))
	}
	if !results[0].Success {
		t.Error("results[0].Success = false, want true")
	}
}

func TestWorkerPoolExecuteToolCallsMultiple(t *testing.T) {
	wp := NewWorkerPool(3)

	ctx := context.Background()
	executor := func(ctx context.Context, call ToolCallInput) sdk.ToolResult {
		time.Sleep(10 * time.Millisecond)
		return sdk.ToolResult{Success: true, Data: map[string]any{"id": call.ID}}
	}

	calls := []ToolCallInput{
		{ID: "call1", Name: "test1", Arguments: nil},
		{ID: "call2", Name: "test2", Arguments: nil},
		{ID: "call3", Name: "test3", Arguments: nil},
	}
	results := wp.ExecuteToolCalls(ctx, calls, executor)

	if len(results) != 3 {
		t.Fatalf("len(results) = %d, want 3", len(results))
	}
	for i, r := range results {
		if !r.Success {
			t.Errorf("results[%d].Success = false, want true", i)
		}
	}
}

func TestWorkerPoolExecuteToolCallsContextCancel(t *testing.T) {
	wp := NewWorkerPool(3)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	executor := func(ctx context.Context, call ToolCallInput) sdk.ToolResult {
		time.Sleep(100 * time.Millisecond)
		return sdk.ToolResult{Success: true}
	}

	calls := []ToolCallInput{
		{ID: "call1", Name: "test1", Arguments: nil},
		{ID: "call2", Name: "test2", Arguments: nil},
	}
	results := wp.ExecuteToolCalls(ctx, calls, executor)

	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
	for _, r := range results {
		if r.Success {
			t.Error("all results should have Success=false due to context cancel")
		}
	}
}

func TestWorkerPoolSingleWorker(t *testing.T) {
	wp := NewWorkerPool(1)

	ctx := context.Background()
	executor := func(ctx context.Context, call ToolCallInput) sdk.ToolResult {
		return sdk.ToolResult{Success: true}
	}

	calls := []ToolCallInput{
		{ID: "call1", Name: "test1", Arguments: nil},
		{ID: "call2", Name: "test2", Arguments: nil},
	}
	results := wp.ExecuteToolCalls(ctx, calls, executor)

	if len(results) != 2 {
		t.Fatalf("len(results) = %d, want 2", len(results))
	}
}

func TestWorkerPoolClose(t *testing.T) {
	wp := NewWorkerPool(3)

	ctx := context.Background()
	executor := func(ctx context.Context, call ToolCallInput) sdk.ToolResult {
		time.Sleep(10 * time.Millisecond)
		return sdk.ToolResult{Success: true}
	}

	calls := []ToolCallInput{
		{ID: "call1", Name: "test1", Arguments: nil},
	}
	wp.ExecuteToolCalls(ctx, calls, executor)

	wp.Close()
}

func TestDefaultWorkerPool(t *testing.T) {
	if DefaultWorkerPool == nil {
		t.Fatal("DefaultWorkerPool is nil")
	}
	if DefaultWorkerPool.workers != 3 {
		t.Errorf("DefaultWorkerPool.workers = %d, want 3", DefaultWorkerPool.workers)
	}
}
