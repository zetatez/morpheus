package service

import (
	"github.com/zetatez/morpheus/internal/effect"
)

type Service interface {
	ServiceName() string
}

type BusService struct {
	bus *effect.Bus
}

func NewBusService() *BusService {
	return &BusService{bus: effect.NewBus()}
}

func (s *BusService) Bus() *effect.Bus {
	return s.bus
}

func (s *BusService) Publish(event string, data any) {
	s.bus.Publish(event, data)
}

func (s *BusService) Subscribe(event string, handler effect.EventHandler) *effect.EventSubscription {
	return s.bus.Subscribe(event, handler)
}

func (s *BusService) Unsubscribe(sub *effect.EventSubscription) {
	s.bus.Unsubscribe(sub)
}

func (s *BusService) ServiceName() string {
	return "@morpheus/Bus"
}

func BusServiceLayer() *effect.Layer[*BusService] {
	return effect.LayerFunc(func(ctx *effect.Context) (*BusService, error) {
		return NewBusService(), nil
	})
}

func Provide[Wanted, Provided any](ctx *effect.Context, service *Provided) *effect.Context {
	return ctx.WithService((*Wanted)(nil), *service)
}
