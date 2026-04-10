package app

import (
	"context"
	"sync"
)

type IterationBudget struct {
	maxTotal int
	used     int
	mu       sync.Mutex
}

func NewIterationBudget(maxTotal int) *IterationBudget {
	return &IterationBudget{
		maxTotal: maxTotal,
		used:     0,
	}
}

func (b *IterationBudget) Consume() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used >= b.maxTotal {
		return false
	}
	b.used++
	return true
}

func (b *IterationBudget) Refund() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used > 0 {
		b.used--
	}
}

func (b *IterationBudget) Remaining() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.maxTotal - b.used
}

func (b *IterationBudget) Used() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}

func (b *IterationBudget) Total() int {
	return b.maxTotal
}

func (b *IterationBudget) Exhausted() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used >= b.maxTotal
}

type SharedBudget struct {
	mu      sync.Mutex
	budgets map[string]*IterationBudget
}

func NewSharedBudget() *SharedBudget {
	return &SharedBudget{
		budgets: make(map[string]*IterationBudget),
	}
}

func (s *SharedBudget) Register(id string, maxIterations int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.budgets[id]; !exists {
		s.budgets[id] = NewIterationBudget(maxIterations)
	}
}

func (s *SharedBudget) Get(id string) *IterationBudget {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.budgets[id]
}

func (s *SharedBudget) Consume(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	budget, ok := s.budgets[id]
	if !ok || budget == nil {
		return false
	}
	return budget.Consume()
}

func (s *SharedBudget) Refund(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	budget, ok := s.budgets[id]
	if !ok || budget == nil {
		return
	}
	budget.Refund()
}

func (s *SharedBudget) Remaining(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	budget, ok := s.budgets[id]
	if !ok || budget == nil {
		return 0
	}
	return budget.Remaining()
}

func (s *SharedBudget) Remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.budgets, id)
}

type BudgetContextKey struct{}

func WithBudget(ctx context.Context, budget *IterationBudget) context.Context {
	return context.WithValue(ctx, BudgetContextKey{}, budget)
}

func BudgetFromContext(ctx context.Context) *IterationBudget {
	budget, ok := ctx.Value(BudgetContextKey{}).(*IterationBudget)
	if !ok {
		return nil
	}
	return budget
}

func ConsumeFromContext(ctx context.Context) bool {
	budget := BudgetFromContext(ctx)
	if budget == nil {
		return true
	}
	return budget.Consume()
}

func RefundFromContext(ctx context.Context) {
	budget := BudgetFromContext(ctx)
	if budget == nil {
		return
	}
	budget.Refund()
}

func BudgetRemainingFromContext(ctx context.Context) int {
	budget := BudgetFromContext(ctx)
	if budget == nil {
		return -1
	}
	return budget.Remaining()
}
