package effect

import (
	"fmt"
	"reflect"
	"sync"
)

type Context struct {
	services *baseServiceMap
	parent   *Context
	values   map[any]any
	mu       sync.RWMutex
}

type baseServiceMap struct {
	services map[string]any
	mu       sync.RWMutex
}

func serviceKey(service any) string {
	if service == nil {
		return "nil"
	}
	return reflect.TypeOf(service).String()
}

func (m *baseServiceMap) Get(service any) any {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.services[serviceKey(service)]
}

func (m *baseServiceMap) Set(service, instance any) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.services == nil {
		m.services = make(map[string]any)
	}
	m.services[serviceKey(service)] = instance
}

func NewServiceMap() *baseServiceMap {
	return &baseServiceMap{
		services: make(map[string]any),
	}
}

func NewContext() *Context {
	return &Context{
		services: NewServiceMap(),
		values:   make(map[any]any),
	}
}

func (c *Context) WithService(service, instance any) *Context {
	newCtx := &Context{
		services: c.services,
		parent:   c,
		values:   make(map[any]any),
	}
	newCtx.services.Set(service, instance)
	return newCtx
}

func (c *Context) Get(service any) any {
	if c == nil {
		return nil
	}
	if v := c.services.Get(service); v != nil {
		return v
	}
	if c.parent != nil {
		return c.parent.Get(service)
	}
	return nil
}

func (c *Context) SetValue(key, value any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values[key] = value
}

func (c *Context) GetValue(key any) any {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if v, ok := c.values[key]; ok {
		return v
	}
	if c.parent != nil {
		return c.parent.GetValue(key)
	}
	return nil
}

func (c *Context) Parent() *Context {
	return c.parent
}

type Effect[Out any] func(ctx *Context) (Out, error)

func Run[Out any](effect Effect[Out], ctx *Context) (Out, error) {
	return effect(ctx)
}

func FlatMap[In, Out any](
	effect Effect[In],
	transform func(In) Effect[Out],
) Effect[Out] {
	return func(ctx *Context) (Out, error) {
		val, err := effect(ctx)
		if err != nil {
			var zero Out
			return zero, err
		}
		return transform(val)(ctx)
	}
}

func Map[In, Out any](
	effect Effect[In],
	transform func(In) Out,
) Effect[Out] {
	return func(ctx *Context) (Out, error) {
		val, err := effect(ctx)
		if err != nil {
			var zero Out
			return zero, err
		}
		return transform(val), nil
	}
}

func AndThen[In, Out any](
	effect Effect[In],
	next Effect[Out],
) Effect[Out] {
	return FlatMap(effect, func(_ In) Effect[Out] {
		return next
	})
}

func Gen[Out any](
	body func() Effect[Out],
) Effect[Out] {
	return func(ctx *Context) (Out, error) {
		return body()(ctx)
	}
}

func Promise[Out any](
	fn func() (Out, error),
) Effect[Out] {
	return func(ctx *Context) (Out, error) {
		return fn()
	}
}

type TaggedError[T any] struct {
	Tag   string
	Value T
}

func (e *TaggedError[T]) Error() string {
	return fmt.Sprintf("%s: %v", e.Tag, e.Value)
}

func (e *TaggedError[T]) Unwrap() T {
	return e.Value
}

func NewTaggedError[T any](tag string, value T) *TaggedError[T] {
	return &TaggedError[T]{Tag: tag, Value: value}
}

var _ error = (*TaggedError[any])(nil)

type Deferred[T any] struct {
	value T
	err   error
	done  chan struct{}
	once  sync.Once
}

func NewDeferred[T any]() *Deferred[T] {
	return &Deferred[T]{done: make(chan struct{})}
}

func (d *Deferred[T]) Complete(val T) {
	d.once.Do(func() {
		d.value = val
		close(d.done)
	})
}

func (d *Deferred[T]) Fail(err error) {
	d.once.Do(func() {
		d.err = err
		close(d.done)
	})
}

func (d *Deferred[T]) Await() (T, error) {
	<-d.done
	return d.value, d.err
}

func (d *Deferred[T]) IsDone() bool {
	select {
	case <-d.done:
		return true
	default:
		return false
	}
}

func (d *Deferred[T]) Get() (T, bool) {
	select {
	case <-d.done:
		return d.value, d.err == nil
	default:
		var zero T
		return zero, false
	}
}

type Option[T any] struct {
	value T
	some  bool
}

func Some[T any](v T) Option[T] {
	return Option[T]{value: v, some: true}
}

func None[T any]() Option[T] {
	return Option[T]{}
}

func (o Option[T]) IsSome() bool {
	return o.some
}

func (o Option[T]) IsNone() bool {
	return !o.some
}

func (o Option[T]) Unwrap() T {
	return o.value
}

func (o Option[T]) UnwrapOr(defaultVal T) T {
	if o.some {
		return o.value
	}
	return defaultVal
}

type Result[T any] struct {
	value T
	err   error
	ok    bool
}

func Ok[T any](v T) Result[T] {
	return Result[T]{value: v, ok: true}
}

func Err[T any](err error) Result[T] {
	return Result[T]{err: err, ok: false}
}

func (r Result[T]) IsOk() bool {
	return r.ok
}

func (r Result[T]) IsErr() bool {
	return !r.ok
}

func (r Result[T]) Unwrap() (T, error) {
	if r.ok {
		return r.value, nil
	}
	var zero T
	return zero, r.err
}

func (r Result[T]) OrElse(val T) T {
	if r.ok {
		return r.value
	}
	return val
}

type Layer[Service any] struct {
	build func(ctx *Context) (Service, error)
}

func LayerFunc[Service any](build func(ctx *Context) (Service, error)) *Layer[Service] {
	return &Layer[Service]{build: build}
}

func (l *Layer[Service]) Build(ctx *Context) (Service, error) {
	return l.build(ctx)
}

func Provide[Wanted, Provided any](ctx *Context, service *Provided) *Context {
	return ctx.WithService((*Wanted)(nil), *service)
}

type Service[Service any] interface {
	Marker() *Service
}

type ServiceOf[Service any] struct{}

func (s *ServiceOf[Service]) Marker() *Service {
	var zero Service
	return &zero
}

func ServiceMarker[Service any]() *Service {
	var zero Service
	return &zero
}

type Layered[Outer, Inner any] struct {
	Outer Outer
	Inner Inner
}

func MakeLayer[Service any](build func(ctx *Context) (Service, error)) *Layer[Service] {
	return LayerFunc(build)
}

func CombineLayers[Outer, Inner any](
	outer *Layer[Outer],
	inner *Layer[Inner],
) *Layer[Outer] {
	return LayerFunc(func(ctx *Context) (Outer, error) {
		_, err := inner.Build(ctx)
		if err != nil {
			var zero Outer
			return zero, err
		}
		return outer.Build(ctx)
	})
}

type Fn[In, Out any] func(In) Out

func Identity[T any](v T) T {
	return v
}

type Pipe[In, Out any] struct {
	value In
	fn    Fn[In, Out]
}

func PipeTo[In, Out any](v In, fn Fn[In, Out]) Out {
	return fn(v)
}

func (p Pipe[In, Out]) Then(next Fn[Out, Out]) Pipe[In, Out] {
	return Pipe[In, Out]{value: p.value, fn: func(i In) Out { return next(p.fn(i)) }}
}

type Stream[Out any] struct {
	produce func() (Out, bool)
}

func Of[Out any](produce func() (Out, bool)) Stream[Out] {
	return Stream[Out]{produce: produce}
}

func (s Stream[Out]) Iterate(fn func(Out) bool) {
	for v, ok := s.produce(); ok; v, ok = s.produce() {
		if !fn(v) {
			break
		}
	}
}

func FromSlice[T any](items []T) Stream[T] {
	idx := 0
	return Stream[T]{
		produce: func() (T, bool) {
			if idx >= len(items) {
				var zero T
				return zero, false
			}
			v := items[idx]
			idx++
			return v, true
		},
	}
}

func StreamMap[In, Out2 any](s Stream[In], fn func(In) Out2) Stream[Out2] {
	return Stream[Out2]{
		produce: func() (Out2, bool) {
			v, ok := s.produce()
			if !ok {
				var zero Out2
				return zero, false
			}
			return fn(v), true
		},
	}
}

func StreamFilter[Out any](s Stream[Out], pred func(Out) bool) Stream[Out] {
	return Stream[Out]{
		produce: func() (Out, bool) {
			for {
				v, ok := s.produce()
				if !ok {
					return v, false
				}
				if pred(v) {
					return v, true
				}
			}
		},
	}
}

func (s Stream[Out]) Take(n int) Stream[Out] {
	count := 0
	return Stream[Out]{
		produce: func() (Out, bool) {
			if count >= n {
				var zero Out
				return zero, false
			}
			count++
			return s.produce()
		},
	}
}

func (s Stream[Out]) ToSlice() []Out {
	var result []Out
	s.Iterate(func(v Out) bool {
		result = append(result, v)
		return true
	})
	return result
}

type Cause[Out any] struct {
	Value any
	Fail  bool
}

func SuccessCause[Out any](v Out) Cause[Out] {
	return Cause[Out]{Value: v, Fail: false}
}

func FailureCause[Out any](e error) Cause[Out] {
	return Cause[Out]{Value: e, Fail: true}
}

func (c Cause[Out]) IsFail() bool {
	return c.Fail
}

func (c Cause[Out]) IsSuccess() bool {
	return !c.Fail
}

func (c Cause[Out]) Unwrap() (Out, error) {
	if c.Fail {
		var zero Out
		return zero, c.Value.(error)
	}
	return c.Value.(Out), nil
}

func Catch[Out any](effect Effect[Out], handler func(error) Out) Out {
	v, err := effect(NewContext())
	if err != nil {
		return handler(err)
	}
	return v
}

func OrDie[Out any](effect Effect[Out]) Out {
	v, err := effect(NewContext())
	if err != nil {
		panic(err)
	}
	return v
}

func OrElseEffect[Out any](effect Effect[Out], defaultVal Out) Out {
	v, err := effect(NewContext())
	if err != nil {
		return defaultVal
	}
	return v
}

func Attempt[Out any](fn func() error) Effect[Out] {
	return func(ctx *Context) (Out, error) {
		err := fn()
		if err != nil {
			var zero Out
			return zero, err
		}
		var zero Out
		return zero, nil
	}
}

func FromContext[Out any](fn func(*Context) Out) Effect[Out] {
	return func(ctx *Context) (Out, error) {
		return fn(ctx), nil
	}
}

func Sync[Out any](fn func() Out) Effect[Out] {
	return func(ctx *Context) (Out, error) {
		return fn(), nil
	}
}

func Fail[Out any](err error) Effect[Out] {
	return func(ctx *Context) (Out, error) {
		var zero Out
		return zero, err
	}
}

func Succeed[Out any](v Out) Effect[Out] {
	return func(ctx *Context) (Out, error) {
		return v, nil
	}
}

func FirstSuccessOf[Out any](effects ...Effect[Out]) Effect[Out] {
	return func(ctx *Context) (Out, error) {
		for _, effect := range effects {
			v, err := effect(ctx)
			if err == nil {
				return v, nil
			}
		}
		var zero Out
		return zero, fmt.Errorf("all effects failed")
	}
}

func Retry[Out any](effect Effect[Out], maxRetries int, delayMs int) Effect[Out] {
	return func(ctx *Context) (Out, error) {
		var lastErr error
		for i := 0; i < maxRetries; i++ {
			v, err := effect(ctx)
			if err == nil {
				return v, nil
			}
			lastErr = err
			if i < maxRetries-1 {
			}
		}
		var zero Out
		return zero, lastErr
	}
}

type ServiceMapInterface interface {
	Get(service any) any
}

type Runtime struct {
	ctx    *Context
	layers []func(*Context) error
}

func NewRuntime() *Runtime {
	return &Runtime{
		ctx: NewContext(),
	}
}

func (r *Runtime) Provide(service, instance any) *Runtime {
	r.ctx = r.ctx.WithService(service, instance)
	return r
}

func (r *Runtime) Build() *Context {
	return r.ctx
}

func (r *Runtime) Run(effect Effect[any]) (any, error) {
	return effect(r.ctx)
}
