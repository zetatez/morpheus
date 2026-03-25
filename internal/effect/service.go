package effect

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

type EventID int64

var eventIDCounter int64

func nextEventID() EventID {
	return EventID(atomic.AddInt64(&eventIDCounter, 1))
}

type EventHandler func(data any)

type EventSubscription struct {
	id        EventID
	event     string
	handler   EventHandler
	active    bool
	createdAt time.Time
	metadata  map[string]any
}

type Bus struct {
	mu            sync.RWMutex
	subscriptions map[string][]*EventSubscription
}

func NewBus() *Bus {
	return &Bus{
		subscriptions: make(map[string][]*EventSubscription),
	}
}

func (b *Bus) Subscribe(event string, handler EventHandler) *EventSubscription {
	return b.SubscribeWithMetadata(event, handler, nil)
}

func (b *Bus) SubscribeWithMetadata(event string, handler EventHandler, metadata map[string]any) *EventSubscription {
	b.mu.Lock()
	defer b.mu.Unlock()

	sub := &EventSubscription{
		id:        nextEventID(),
		event:     event,
		handler:   handler,
		active:    true,
		createdAt: time.Now(),
		metadata:  metadata,
	}

	b.subscriptions[event] = append(b.subscriptions[event], sub)
	return sub
}

func (b *Bus) Unsubscribe(sub *EventSubscription) {
	b.mu.Lock()
	defer b.mu.Unlock()

	subs := b.subscriptions[sub.event]
	for i, s := range subs {
		if s.id == sub.id {
			s.active = false
			b.subscriptions[sub.event] = append(subs[:i], subs[i+1:]...)
			return
		}
	}
}

func (b *Bus) UnsubscribeAll(event string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.subscriptions, event)
}

func (b *Bus) Publish(event string, data any) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, sub := range b.subscriptions[event] {
		if sub.active {
			go sub.handler(data)
		}
	}
}

func (b *Bus) PublishSync(event string, data any) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, sub := range b.subscriptions[event] {
		if sub.active {
			sub.handler(data)
		}
	}
}

func (b *Bus) PublishWithSource(event string, source string, data any) {
	b.mu.RLock()
	defer b.mu.RUnlock()

	for _, sub := range b.subscriptions[event] {
		if sub.active {
			if sub.metadata != nil {
				if src, ok := sub.metadata["source"].(string); ok && src != "" && src != source {
					continue
				}
			}
			go sub.handler(data)
		}
	}
}

func (b *Bus) ActiveCount(event string) int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscriptions[event])
}

func (b *Bus) ListEvents() []string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	events := make([]string, 0, len(b.subscriptions))
	for event := range b.subscriptions {
		events = append(events, event)
	}
	return events
}

type EventBus interface {
	Publish(event string, data any)
	Subscribe(event string, handler EventHandler) *EventSubscription
	Unsubscribe(sub *EventSubscription)
}

var _ EventBus = (*Bus)(nil)

type ScopedBus struct {
	instanceID string
	bus        *Bus
	parent     *GlobalBus
}

func NewScopedBus(instanceID string, parent *GlobalBus) *ScopedBus {
	return &ScopedBus{
		instanceID: instanceID,
		bus:        NewBus(),
		parent:     parent,
	}
}

func (s *ScopedBus) Publish(event string, data any) {
	s.bus.Publish(event, data)
	if s.parent != nil {
		s.parent.publishFromInstance(s.instanceID, event, data)
	}
}

func (s *ScopedBus) Subscribe(event string, handler EventHandler) *EventSubscription {
	return s.bus.Subscribe(event, handler)
}

func (s *ScopedBus) SubscribeWithMetadata(event string, handler EventHandler, metadata map[string]any) *EventSubscription {
	return s.bus.SubscribeWithMetadata(event, handler, metadata)
}

func (s *ScopedBus) Unsubscribe(sub *EventSubscription) {
	s.bus.Unsubscribe(sub)
}

func (s *ScopedBus) PublishGlobal(event string, data any) {
	if s.parent != nil {
		s.parent.Publish(event, data)
	}
}

func (s *ScopedBus) SubscribeGlobal(event string, handler EventHandler) *EventSubscription {
	if s.parent != nil {
		return s.parent.Subscribe(event, handler)
	}
	return nil
}

type GlobalBus struct {
	mu          sync.RWMutex
	buses       map[string]*ScopedBus
	globalBus   *Bus
	instanceBus *Bus
}

var globalBusInstance *GlobalBus
var globalBusOnce sync.Once

func GetGlobalBus() *GlobalBus {
	globalBusOnce.Do(func() {
		globalBusInstance = &GlobalBus{
			buses:       make(map[string]*ScopedBus),
			globalBus:   NewBus(),
			instanceBus: NewBus(),
		}
	})
	return globalBusInstance
}

func (g *GlobalBus) CreateScopedBus(instanceID string) *ScopedBus {
	g.mu.Lock()
	defer g.mu.Unlock()

	if scoped, exists := g.buses[instanceID]; exists {
		return scoped
	}

	scoped := NewScopedBus(instanceID, g)
	g.buses[instanceID] = scoped
	return scoped
}

func (g *GlobalBus) GetScopedBus(instanceID string) (*ScopedBus, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	scoped, exists := g.buses[instanceID]
	return scoped, exists
}

func (g *GlobalBus) DisposeScopedBus(instanceID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if scoped, exists := g.buses[instanceID]; exists {
		events := scoped.bus.ListEvents()
		for _, event := range events {
			scoped.bus.UnsubscribeAll(event)
		}
		delete(g.buses, instanceID)
	}
}

func (g *GlobalBus) Publish(event string, data any) {
	g.globalBus.Publish(event, data)
}

func (g *GlobalBus) PublishInstance(event string, data any) {
	g.instanceBus.Publish(event, data)
}

func (g *GlobalBus) publishFromInstance(instanceID, event string, data any) {
	g.globalBus.Publish(event, data)
}

func (g *GlobalBus) Subscribe(event string, handler EventHandler) *EventSubscription {
	return g.globalBus.Subscribe(event, handler)
}

func (g *GlobalBus) SubscribeInstance(event string, handler EventHandler) *EventSubscription {
	return g.instanceBus.Subscribe(event, handler)
}

func (g *GlobalBus) Unsubscribe(sub *EventSubscription) {
	g.globalBus.Unsubscribe(sub)
}

func (g *GlobalBus) UnsubscribeInstance(sub *EventSubscription) {
	g.instanceBus.Unsubscribe(sub)
}

func (g *GlobalBus) SubscribeAll(handler EventHandler) []*EventSubscription {
	g.mu.RLock()
	defer g.mu.RUnlock()

	var subs []*EventSubscription
	for _, event := range g.globalBus.ListEvents() {
		sub := g.globalBus.Subscribe(event, handler)
		subs = append(subs, sub)
	}
	return subs
}

func (g *GlobalBus) ActiveInstanceCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.buses)
}

type TaggedBusEvent[Data any] struct {
	Type      string
	Source    string
	Data      Data
	Timestamp time.Time
}

func NewTaggedBusEvent[Data any](eventType string, source string, data Data) TaggedBusEvent[Data] {
	return TaggedBusEvent[Data]{
		Type:      eventType,
		Source:    source,
		Data:      data,
		Timestamp: time.Now(),
	}
}

func DefineEvent[Data any](eventType string) *EventDefinition[Data] {
	return &EventDefinition[Data]{Type: eventType}
}

type EventValidator func(data any) error

type EventDefinition[Data any] struct {
	Type      string
	Validator EventValidator
	Schema    map[string]any
}

func (e *EventDefinition[Data]) Name() string {
	return e.Type
}

func (e *EventDefinition[Data]) Of(data Data) TaggedBusEvent[Data] {
	return TaggedBusEvent[Data]{Type: e.Type, Data: data}
}

func (e *EventDefinition[Data]) WithSource(source string, data Data) TaggedBusEvent[Data] {
	return NewTaggedBusEvent(e.Type, source, data)
}

func (e *EventDefinition[Data]) WithValidator(fn EventValidator) *EventDefinition[Data] {
	e.Validator = fn
	return e
}

func (e *EventDefinition[Data]) WithSchema(schema map[string]any) *EventDefinition[Data] {
	e.Schema = schema
	return e
}

func (e *EventDefinition[Data]) Validate(data Data) error {
	if e.Validator == nil {
		return nil
	}
	return e.Validator(data)
}

func (e *EventDefinition[Data]) Publish(bus *Bus, data Data) error {
	if err := e.Validate(data); err != nil {
		return err
	}
	event := e.Of(data)
	bus.Publish(e.Type, event)
	return nil
}

func (e *EventDefinition[Data]) PublishWithSource(bus *Bus, source string, data Data) error {
	if err := e.Validate(data); err != nil {
		return err
	}
	event := e.WithSource(source, data)
	bus.PublishWithSource(e.Type, source, event)
	return nil
}

type EventRegistry struct {
	mu       sync.RWMutex
	defs     map[string]*EventDefinition[any]
	validate bool
}

func NewEventRegistry() *EventRegistry {
	return &EventRegistry{
		defs:     make(map[string]*EventDefinition[any]),
		validate: true,
	}
}

func (r *EventRegistry) Register(def *EventDefinition[any]) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.defs[def.Type] = def
}

func (r *EventRegistry) Get(eventType string) (*EventDefinition[any], bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.defs[eventType]
	return def, ok
}

func (r *EventRegistry) List() []*EventDefinition[any] {
	r.mu.RLock()
	defer r.mu.RUnlock()
	defs := make([]*EventDefinition[any], 0, len(r.defs))
	for _, def := range r.defs {
		defs = append(defs, def)
	}
	return defs
}

func (r *EventRegistry) ValidateEvent(eventType string, data any) error {
	r.mu.RLock()
	defer r.mu.RUnlock()
	def, ok := r.defs[eventType]
	if !ok {
		return fmt.Errorf("unknown event type: %s", eventType)
	}
	return def.Validate(data)
}

func (r *EventRegistry) SetValidation(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.validate = enabled
}

var globalEventRegistry *EventRegistry

func init() {
	globalEventRegistry = NewEventRegistry()
}

func GetEventRegistry() *EventRegistry {
	return globalEventRegistry
}

type TypedBus[Data any] struct {
	bus *Bus
	def *EventDefinition[Data]
}

func NewTypedBus[Data any](def *EventDefinition[Data]) *TypedBus[Data] {
	return &TypedBus[Data]{
		bus: NewBus(),
		def: def,
	}
}

func (b *TypedBus[Data]) Publish(data Data) error {
	return b.def.Publish(b.bus, data)
}

func (b *TypedBus[Data]) PublishWithSource(source string, data Data) error {
	return b.def.PublishWithSource(b.bus, source, data)
}

func (b *TypedBus[Data]) Subscribe(handler func(TaggedBusEvent[Data])) *EventSubscription {
	return b.bus.Subscribe(b.def.Type, func(data any) {
		if event, ok := data.(TaggedBusEvent[Data]); ok {
			handler(event)
		}
	})
}

func (b *TypedBus[Data]) SubscribeWithSource(handler func(source string, data Data)) *EventSubscription {
	return b.bus.SubscribeWithMetadata(b.def.Type, func(data any) {
		if event, ok := data.(TaggedBusEvent[Data]); ok {
			handler(event.Source, event.Data)
		}
	}, nil)
}

type EventBusService struct {
	bus    *Bus
	global *GlobalBus
}

func NewEventBusService() *EventBusService {
	return &EventBusService{
		bus:    NewBus(),
		global: GetGlobalBus(),
	}
}

func (s *EventBusService) Bus() *Bus {
	return s.bus
}

func (s *EventBusService) Global() *GlobalBus {
	return s.global
}

func (s *EventBusService) Publish(event string, data any) {
	s.bus.Publish(event, data)
}

func (s *EventBusService) PublishGlobal(event string, data any) {
	s.global.Publish(event, data)
}

func (s *EventBusService) Subscribe(event string, handler EventHandler) *EventSubscription {
	return s.bus.Subscribe(event, handler)
}

func (s *EventBusService) SubscribeGlobal(event string, handler EventHandler) *EventSubscription {
	return s.global.Subscribe(event, handler)
}

func (s *EventBusService) Unsubscribe(sub *EventSubscription) {
	s.bus.Unsubscribe(sub)
}

func (s *EventBusService) CreateScopedBus(instanceID string) *ScopedBus {
	return s.global.CreateScopedBus(instanceID)
}

func ServiceOfEventBus() any {
	return (*EventBusService)(nil)
}

type ServiceInterface interface {
	ServiceName() string
}

type TaggedService[Name string] struct{}

func (s *TaggedService[Name]) ServiceName() string {
	return fmt.Sprintf("%v", s)
}

type LayerContext struct {
	ctx      *Context
	services map[string]any
	mu       sync.RWMutex
}

func NewLayerContext() *LayerContext {
	return &LayerContext{
		ctx:      NewContext(),
		services: make(map[string]any),
	}
}

func (lc *LayerContext) Set(name string, service any) {
	lc.mu.Lock()
	defer lc.mu.Unlock()
	lc.services[name] = service
	lc.ctx = lc.ctx.WithService(nil, service)
}

func (lc *LayerContext) Get(name string) any {
	lc.mu.RLock()
	defer lc.mu.RUnlock()
	return lc.services[name]
}

func (lc *LayerContext) Context() *Context {
	return lc.ctx
}

type ServiceBuilder[Service any] struct {
	name  string
	build func(ctx *Context) (Service, error)
	deps  []any
}

func NewServiceBuilder[Service any](name string) *ServiceBuilder[Service] {
	return &ServiceBuilder[Service]{name: name}
}

func (b *ServiceBuilder[Service]) WithDeps(deps ...any) *ServiceBuilder[Service] {
	b.deps = deps
	return b
}

func (b *ServiceBuilder[Service]) Build(build func(ctx *Context) (Service, error)) *ServiceBuilder[Service] {
	b.build = build
	return b
}

func (b *ServiceBuilder[Service]) Create() *Layer[Service] {
	return LayerFunc(func(ctx *Context) (Service, error) {
		for _, dep := range b.deps {
			if serviceDep, ok := dep.(interface{ ServiceName() string }); ok {
				svc := ctx.Get(serviceDep)
				if svc == nil {
					var zero Service
					return zero, fmt.Errorf("service %s not found in context", serviceDep.ServiceName())
				}
			}
		}
		return b.build(ctx)
	})
}

type EffectService[Service any] struct {
	service Service
}

func (s *EffectService[Service]) Service() Service {
	return s.service
}

func RunServices[Out any](
	effects ...Effect[any],
) (Out, error) {
	ctx := NewContext()

	for _, effect := range effects {
		_, err := effect(ctx)
		if err != nil {
			var zero Out
			return zero, err
		}
	}

	var zero Out
	return zero, nil
}

type Finalizer func() error

type ContextWithFinalizer struct {
	*Context
	finalizers []Finalizer
	mu         sync.Mutex
}

func NewContextWithFinalizer() *ContextWithFinalizer {
	return &ContextWithFinalizer{
		Context:    NewContext(),
		finalizers: make([]Finalizer, 0),
	}
}

func (c *ContextWithFinalizer) AddFinalizer(f Finalizer) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.finalizers = append(c.finalizers, f)
}

func (c *ContextWithFinalizer) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	var lastErr error
	for _, f := range c.finalizers {
		if err := f(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

type AcquireRelease[Resource any] struct {
	acquire func() (Resource, error)
	release func(Resource) error
}

func Acquire[Resource any](acquire func() (Resource, error), release func(Resource) error) *AcquireRelease[Resource] {
	return &AcquireRelease[Resource]{
		acquire: acquire,
		release: release,
	}
}

func (ar *AcquireRelease[Resource]) Use(fn func(Resource) Effect[any]) Effect[any] {
	return func(ctx *Context) (any, error) {
		resource, err := ar.acquire()
		if err != nil {
			return nil, err
		}

		if ar.release != nil {
			defer func() {
				_ = ar.release(resource)
			}()
		}

		return fn(resource)(ctx)
	}
}

type Semaphore struct {
	permits  int
	acquired int
	mu       sync.Mutex
}

func NewSemaphore(permits int) *Semaphore {
	return &Semaphore{permits: permits}
}

func (s *Semaphore) Acquire() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.acquired < s.permits {
		s.acquired++
		return true
	}
	return false
}

func (s *Semaphore) Release() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.acquired > 0 {
		s.acquired--
	}
}

func (s *Semaphore) Available() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.permits - s.acquired
}

type WaitGroup struct {
	counter int64
	waiters int64
	sema    chan struct{}
	mu      sync.Mutex
}

func NewWaitGroup() *WaitGroup {
	return &WaitGroup{sema: make(chan struct{}, 1)}
}

func (wg *WaitGroup) Add(delta int) {
	atomic.AddInt64(&wg.counter, int64(delta))
}

func (wg *WaitGroup) Done() {
	wg.Add(-1)
}

func (wg *WaitGroup) Wait() {
	wg.mu.Lock()
	if atomic.LoadInt64(&wg.counter) > 0 {
		wg.waiters++
		wg.mu.Unlock()
		<-wg.sema
	} else {
		wg.mu.Unlock()
	}
}

func (wg *WaitGroup) Signal() {
	wg.mu.Lock()
	if wg.waiters > 0 {
		wg.waiters--
		wg.sema <- struct{}{}
	}
	wg.mu.Unlock()
}
