package effect

import (
	"fmt"
	"testing"
)

func TestContext(t *testing.T) {
	ctx := NewContext()

	type TestService struct {
		Name string
	}

	svc := &TestService{Name: "test"}
	ctx = ctx.WithService((*TestService)(nil), svc)

	got := ctx.Get((*TestService)(nil))
	if got == nil {
		t.Fatal("expected to get service from context")
	}
	s, ok := got.(*TestService)
	if !ok {
		t.Errorf("expected *TestService, got %T", got)
	}
	if s.Name != "test" {
		t.Errorf("expected Name=test, got Name=%s", s.Name)
	}
}

func TestEffectRun(t *testing.T) {
	ctx := NewContext()

	eff := func(ctx *Context) (string, error) {
		return "hello", nil
	}

	result, err := Run(eff, ctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != "hello" {
		t.Errorf("expected hello, got %s", result)
	}
}

func TestEffectMap(t *testing.T) {
	ctx := NewContext()

	eff := func(ctx *Context) (int, error) {
		return 42, nil
	}

	mapped := Map(eff, func(n int) string {
		return string(rune(n))
	})

	result, err := Run(mapped, ctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != "*" { // 42 is ASCII for '*'
		t.Errorf("expected *, got %s", result)
	}
}

func TestDeferred(t *testing.T) {
	d := NewDeferred[int]()

	go func() {
		d.Complete(42)
	}()

	val, err := d.Await()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if val != 42 {
		t.Errorf("expected 42, got %d", val)
	}
}

func TestDeferredFail(t *testing.T) {
	d := NewDeferred[int]()

	go func() {
		d.Fail(fmt.Errorf("test error"))
	}()

	_, err := d.Await()
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestOption(t *testing.T) {
	some := Some(42)
	if !some.IsSome() {
		t.Error("expected Some")
	}
	if some.Unwrap() != 42 {
		t.Errorf("expected 42, got %d", some.Unwrap())
	}

	none := None[int]()
	if !none.IsNone() {
		t.Error("expected None")
	}
	if none.UnwrapOr(100) != 100 {
		t.Error("expected UnwrapOr to return default")
	}
}

func TestResult(t *testing.T) {
	ok := Ok[int](42)
	if !ok.IsOk() {
		t.Error("expected Ok")
	}
	val, err := ok.Unwrap()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if val != 42 {
		t.Errorf("expected 42, got %d", val)
	}

	errRes := Err[int](fmt.Errorf("fail"))
	if !errRes.IsErr() {
		t.Error("expected Err")
	}
	_, err = errRes.Unwrap()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestBus(t *testing.T) {
	bus := NewBus()

	var received any
	sub := bus.Subscribe("test.event", func(data any) {
		received = data
	})

	bus.PublishSync("test.event", "hello")

	if received != "hello" {
		t.Errorf("expected hello, got %v", received)
	}

	bus.Unsubscribe(sub)
	bus.PublishSync("test.event", "should not receive")
	if received != "hello" {
		t.Error("expected old value after unsubscribe")
	}
}

func TestBusAsync(t *testing.T) {
	bus := NewBus()

	done := make(chan bool)
	sub := bus.Subscribe("async.event", func(data any) {
		if data == "async" {
			done <- true
		}
	})

	bus.Publish("async.event", "async")

	select {
	case <-done:
		// success
	case <-make(chan bool):
		t.Error("did not receive async event")
	}

	bus.Unsubscribe(sub)
}

func TestLayer(t *testing.T) {
	type Service struct {
		Name string
	}

	layer := LayerFunc(func(ctx *Context) (Service, error) {
		return Service{Name: "layered"}, nil
	})

	ctx := NewContext()
	svc, err := layer.Build(ctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if svc.Name != "layered" {
		t.Errorf("expected layered, got %s", svc.Name)
	}
}

func TestFlatMap(t *testing.T) {
	ctx := NewContext()

	eff1 := func(ctx *Context) (int, error) {
		return 1, nil
	}

	eff2 := func(n int) Effect[int] {
		return func(ctx *Context) (int, error) {
			return n + 1, nil
		}
	}

	result, err := Run(FlatMap(eff1, eff2), ctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != 2 {
		t.Errorf("expected 2, got %d", result)
	}
}

func TestStream(t *testing.T) {
	items := []int{1, 2, 3}
	stream := FromSlice(items)

	sum := 0
	stream.Iterate(func(v int) bool {
		sum += v
		return true
	})

	if sum != 6 {
		t.Errorf("expected sum=6, got %d", sum)
	}
}

func TestStreamMap(t *testing.T) {
	items := []int{1, 2, 3}
	stream := FromSlice(items)

	mapped := StreamMap(stream, func(n int) string {
		return string(rune('a' + n - 1))
	})

	result := mapped.ToSlice()
	if len(result) != 3 || result[0] != "a" || result[1] != "b" || result[2] != "c" {
		t.Errorf("expected [a,b,c], got %v", result)
	}
}

func TestStreamFilter(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	stream := FromSlice(items)

	filtered := StreamFilter(stream, func(n int) bool {
		return n%2 == 1
	})

	result := filtered.ToSlice()
	if len(result) != 3 || result[0] != 1 || result[1] != 3 || result[2] != 5 {
		t.Errorf("expected [1,3,5], got %v", result)
	}
}

func TestStreamTake(t *testing.T) {
	items := []int{1, 2, 3, 4, 5}
	stream := FromSlice(items)

	taken := stream.Take(3)

	result := taken.ToSlice()
	if len(result) != 3 {
		t.Errorf("expected 3 items, got %d", len(result))
	}
}

func TestAndThen(t *testing.T) {
	ctx := NewContext()

	first := func(ctx *Context) (int, error) {
		return 10, nil
	}

	second := func(ctx *Context) (int, error) {
		return 20, nil
	}

	result, err := Run(AndThen(first, second), ctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != 20 {
		t.Errorf("expected 20, got %d", result)
	}
}

func TestPromise(t *testing.T) {
	ctx := NewContext()

	eff := Promise(func() (int, error) {
		return 100, nil
	})

	result, err := Run(eff, ctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if result != 100 {
		t.Errorf("expected 100, got %d", result)
	}
}

func TestOrElse(t *testing.T) {
	fail := func(ctx *Context) (int, error) {
		return 0, fmt.Errorf("fail")
	}

	result := OrElseEffect(fail, 99)
	if result != 99 {
		t.Errorf("expected 99, got %d", result)
	}
}

func TestOrDie(t *testing.T) {
	succeed := func(ctx *Context) (int, error) {
		return 42, nil
	}

	result := OrDie(succeed)
	if result != 42 {
		t.Errorf("expected 42, got %d", result)
	}
}

func TestSemaphore(t *testing.T) {
	sem := NewSemaphore(2)

	if sem.Available() != 2 {
		t.Errorf("expected 2 available, got %d", sem.Available())
	}

	if !sem.Acquire() {
		t.Error("expected acquire to succeed")
	}
	if sem.Available() != 1 {
		t.Errorf("expected 1 available, got %d", sem.Available())
	}

	if !sem.Acquire() {
		t.Error("expected acquire to succeed")
	}
	if sem.Available() != 0 {
		t.Errorf("expected 0 available, got %d", sem.Available())
	}

	if sem.Acquire() {
		t.Error("expected acquire to fail")
	}

	sem.Release()
	if sem.Available() != 1 {
		t.Errorf("expected 1 available, got %d", sem.Available())
	}
}

func TestTaggedError(t *testing.T) {
	err := NewTaggedError("test", "value")
	if err.Error() != "test: value" {
		t.Errorf("expected 'test: value', got '%s'", err.Error())
	}
	if err.Unwrap() != "value" {
		t.Errorf("expected 'value', got '%v'", err.Unwrap())
	}
}
