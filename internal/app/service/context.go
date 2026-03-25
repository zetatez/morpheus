package service

import (
	"github.com/zetatez/morpheus/internal/effect"
)

type RuntimeContext struct {
	*effect.Context
	services map[string]any
}

func NewRuntimeContext() *RuntimeContext {
	return &RuntimeContext{
		Context:  effect.NewContext(),
		services: make(map[string]any),
	}
}

func (c *RuntimeContext) SetService(name string, service any) {
	c.services[name] = service
	c.Context = c.Context.WithService(nil, service)
}

func (c *RuntimeContext) GetService(name string) any {
	return c.services[name]
}

func (c *RuntimeContext) AllServices() map[string]any {
	result := make(map[string]any)
	for k, v := range c.services {
		result[k] = v
	}
	return result
}

type ServiceRegistry struct {
	ctx      *RuntimeContext
	services map[string]any
	layers   []func(*RuntimeContext) error
}

func NewServiceRegistry() *ServiceRegistry {
	return &ServiceRegistry{
		ctx:      NewRuntimeContext(),
		services: make(map[string]any),
	}
}

func (r *ServiceRegistry) Register(name string, service any) *ServiceRegistry {
	r.services[name] = service
	r.ctx.SetService(name, service)
	return r
}

func (r *ServiceRegistry) Get(name string) (any, bool) {
	svc, ok := r.services[name]
	return svc, ok
}

func (r *ServiceRegistry) Context() *RuntimeContext {
	return r.ctx
}

func (r *ServiceRegistry) Build() error {
	for _, layer := range r.layers {
		if err := layer(r.ctx); err != nil {
			return err
		}
	}
	return nil
}

func (r *ServiceRegistry) AddLayer(build func(*RuntimeContext) error) *ServiceRegistry {
	r.layers = append(r.layers, build)
	return r
}

type EffectContext = *RuntimeContext

func GetService[Service any](ctx EffectContext, name string) (Service, bool) {
	svc := ctx.GetService(name)
	if svc == nil {
		var zero Service
		return zero, false
	}
	s, ok := svc.(Service)
	return s, ok
}

func MustGetService[Service any](ctx EffectContext, name string) Service {
	svc, ok := GetService[Service](ctx, name)
	if !ok {
		panic("service not found: " + name)
	}
	return svc
}

type LayeredService[Service any] struct {
	name  string
	layer *effect.Layer[Service]
	inst  Service
}

func (s *LayeredService[Service]) ServiceName() string {
	return s.name
}

func (s *LayeredService[Service]) Layer() *effect.Layer[Service] {
	return s.layer
}

func (s *LayeredService[Service]) Instance() Service {
	return s.inst
}

func (s *LayeredService[Service]) Provide(ctx *effect.Context) *effect.Context {
	return ctx.WithService(s, s.inst)
}

func NewLayeredService[Service any](name string, build func(ctx *effect.Context) (Service, error)) *LayeredService[Service] {
	return &LayeredService[Service]{
		name:  name,
		layer: effect.LayerFunc(build),
	}
}

type ServiceEntry struct {
	Name  string
	Build func(ctx *effect.Context) (any, error)
	Inst  any
}
