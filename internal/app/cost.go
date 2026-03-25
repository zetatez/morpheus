package app

import (
	"sync"
)

type TokenUsage struct {
	InputTokens      int64 `json:"input_tokens"`
	OutputTokens     int64 `json:"output_tokens"`
	ReasoningTokens  int64 `json:"reasoning_tokens,omitempty"`
	CacheReadTokens  int64 `json:"cache_read_tokens,omitempty"`
	CacheWriteTokens int64 `json:"cache_write_tokens,omitempty"`
}

func (t TokenUsage) Total() int64 {
	return t.InputTokens + t.OutputTokens + t.ReasoningTokens + t.CacheReadTokens + t.CacheWriteTokens
}

type ModelPricing struct {
	InputPer1M      float64 `json:"input_per_1m"`
	OutputPer1M     float64 `json:"output_per_1m"`
	ReasoningPer1M  float64 `json:"reasoning_per_1m,omitempty"`
	CacheReadPer1M  float64 `json:"cache_read_per_1m,omitempty"`
	CacheWritePer1M float64 `json:"cache_write_per_1m,omitempty"`
}

var defaultPricing = map[string]ModelPricing{
	"gpt-4o":                     {InputPer1M: 2.50, OutputPer1M: 10.00},
	"gpt-4o-mini":                {InputPer1M: 0.15, OutputPer1M: 0.60},
	"gpt-4-turbo":                {InputPer1M: 10.00, OutputPer1M: 30.00},
	"gpt-4":                      {InputPer1M: 30.00, OutputPer1M: 60.00},
	"o1-preview":                 {InputPer1M: 15.00, OutputPer1M: 60.00, ReasoningPer1M: 60.00},
	"o1-mini":                    {InputPer1M: 3.00, OutputPer1M: 12.00, ReasoningPer1M: 12.00},
	"claude-3-5-sonnet":          {InputPer1M: 3.00, OutputPer1M: 15.00, ReasoningPer1M: 3.00},
	"claude-3-5-sonnet-20241022": {InputPer1M: 3.00, OutputPer1M: 15.00, ReasoningPer1M: 3.00},
	"claude-3-5-haiku":           {InputPer1M: 1.00, OutputPer1M: 5.00},
	"claude-3-opus":              {InputPer1M: 15.00, OutputPer1M: 75.00},
	"claude-3-haiku":             {InputPer1M: 0.25, OutputPer1M: 1.25},
	"claude-3-sonnet":            {InputPer1M: 3.00, OutputPer1M: 15.00},
	"gemini-2.0-flash":           {InputPer1M: 0.10, OutputPer1M: 0.40, CacheReadPer1M: 0.01},
	"gemini-1.5-pro":             {InputPer1M: 1.25, OutputPer1M: 5.00, CacheReadPer1M: 0.10},
	"gemini-1.5-flash":           {InputPer1M: 0.075, OutputPer1M: 0.30, CacheReadPer1M: 0.01},
	"gemini-1.5-flash-8b":        {InputPer1M: 0.0375, OutputPer1M: 0.15, CacheReadPer1M: 0.005},
	"deepseek-chat":              {InputPer1M: 0.14, OutputPer1M: 2.80},
	"deepseek-coder":             {InputPer1M: 0.14, OutputPer1M: 2.80},
	"deepseek-reasoner":          {InputPer1M: 0.14, OutputPer1M: 2.80, ReasoningPer1M: 2.80},
	"llama-3.3-70b":              {InputPer1M: 0.90, OutputPer1M: 2.90},
	"llama-3.2-90b":              {InputPer1M: 0.90, OutputPer1M: 2.90},
	"llama-3.2-11b":              {InputPer1M: 0.30, OutputPer1M: 0.60},
	"llama-3.1-70b":              {InputPer1M: 0.65, OutputPer1M: 2.75},
	"llama-3.1-8b":               {InputPer1M: 0.20, OutputPer1M: 0.40},
	"llama-3-70b":                {InputPer1M: 0.65, OutputPer1M: 2.75},
	"llama-3-8b":                 {InputPer1M: 0.20, OutputPer1M: 0.40},
	"mixtral-8x22b":              {InputPer1M: 0.65, OutputPer1M: 2.75},
	"mistral-large":              {InputPer1M: 2.00, OutputPer1M: 6.00},
	"mistral-7b":                 {InputPer1M: 0.20, OutputPer1M: 0.60},
	"codestral":                  {InputPer1M: 1.00, OutputPer1M: 3.00},
	"qwen-72b":                   {InputPer1M: 0.90, OutputPer1M: 3.60},
	"qwen-32b":                   {InputPer1M: 0.50, OutputPer1M: 2.00},
	"qwen-7b":                    {InputPer1M: 0.20, OutputPer1M: 0.80},
	"qwen-2.5-72b":               {InputPer1M: 0.90, OutputPer1M: 3.60},
	"qwen-2.5-32b":               {InputPer1M: 0.50, OutputPer1M: 2.00},
	"qwen-2.5-7b":                {InputPer1M: 0.20, OutputPer1M: 0.80},
	"grok-2":                     {InputPer1M: 2.00, OutputPer1M: 10.00},
	"grok-2-mini":                {InputPer1M: 0.30, OutputPer1M: 1.50},
	"command-r-plus":             {InputPer1M: 3.00, OutputPer1M: 15.00},
	"command-r":                  {InputPer1M: 0.50, OutputPer1M: 2.50},
}

func GetModelPricing(model string) ModelPricing {
	if pricing, ok := defaultPricing[model]; ok {
		return pricing
	}
	for prefix, pricing := range defaultPricing {
		if len(prefix) < len(model) && model[:len(prefix)] == prefix {
			return pricing
		}
	}
	return ModelPricing{InputPer1M: 1.00, OutputPer1M: 5.00}
}

func (p ModelPricing) CalculateCost(usage TokenUsage) float64 {
	cost := float64(usage.InputTokens) * p.InputPer1M / 1_000_000
	cost += float64(usage.OutputTokens) * p.OutputPer1M / 1_000_000
	if p.ReasoningPer1M > 0 && usage.ReasoningTokens > 0 {
		cost += float64(usage.ReasoningTokens) * p.ReasoningPer1M / 1_000_000
	}
	if p.CacheReadPer1M > 0 && usage.CacheReadTokens > 0 {
		cost += float64(usage.CacheReadTokens) * p.CacheReadPer1M / 1_000_000
	}
	if p.CacheWritePer1M > 0 && usage.CacheWriteTokens > 0 {
		cost += float64(usage.CacheWriteTokens) * p.CacheWritePer1M / 1_000_000
	}
	return cost
}

type StepCost struct {
	StepIndex int        `json:"step_index"`
	Usage     TokenUsage `json:"usage"`
	Cost      float64    `json:"cost"`
}

type CostTracker struct {
	mu         sync.RWMutex
	model      string
	steps      []StepCost
	totalUsage TokenUsage
	totalCost  float64
}

func NewCostTracker(model string) *CostTracker {
	return &CostTracker{
		model: model,
		steps: make([]StepCost, 0),
	}
}

func (c *CostTracker) RecordStep(stepIndex int, usage TokenUsage) {
	c.mu.Lock()
	defer c.mu.Unlock()

	pricing := GetModelPricing(c.model)
	stepCost := pricing.CalculateCost(usage)

	c.steps = append(c.steps, StepCost{
		StepIndex: stepIndex,
		Usage:     usage,
		Cost:      stepCost,
	})

	c.totalUsage.InputTokens += usage.InputTokens
	c.totalUsage.OutputTokens += usage.OutputTokens
	c.totalUsage.ReasoningTokens += usage.ReasoningTokens
	c.totalUsage.CacheReadTokens += usage.CacheReadTokens
	c.totalUsage.CacheWriteTokens += usage.CacheWriteTokens
	c.totalCost += stepCost
}

func (c *CostTracker) TotalUsage() TokenUsage {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.totalUsage
}

func (c *CostTracker) TotalCost() float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.totalCost
}

func (c *CostTracker) Steps() []StepCost {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]StepCost, len(c.steps))
	copy(result, c.steps)
	return result
}

func (c *CostTracker) Snapshot() map[string]any {
	c.mu.RLock()
	defer c.mu.RUnlock()

	steps := make([]map[string]any, len(c.steps))
	for i, s := range c.steps {
		steps[i] = map[string]any{
			"step":   s.StepIndex,
			"input":  s.Usage.InputTokens,
			"output": s.Usage.OutputTokens,
			"cost":   s.Cost,
		}
	}

	return map[string]any{
		"model": c.model,
		"total": map[string]any{
			"input_tokens":       c.totalUsage.InputTokens,
			"output_tokens":      c.totalUsage.OutputTokens,
			"reasoning_tokens":   c.totalUsage.ReasoningTokens,
			"cache_read_tokens":  c.totalUsage.CacheReadTokens,
			"cache_write_tokens": c.totalUsage.CacheWriteTokens,
			"total_tokens":       c.totalUsage.Total(),
			"estimated_cost":     c.totalCost,
		},
		"steps": steps,
	}
}

type RunCost struct {
	mu         sync.RWMutex
	runID      string
	model      string
	steps      []StepCost
	totalUsage TokenUsage
	totalCost  float64
}

func NewRunCost(runID, model string) *RunCost {
	return &RunCost{
		runID: runID,
		model: model,
		steps: make([]StepCost, 0),
	}
}

func (r *RunCost) RecordStep(stepIndex int, usage TokenUsage) {
	r.mu.Lock()
	defer r.mu.Unlock()

	pricing := GetModelPricing(r.model)
	stepCost := pricing.CalculateCost(usage)

	r.steps = append(r.steps, StepCost{
		StepIndex: stepIndex,
		Usage:     usage,
		Cost:      stepCost,
	})

	r.totalUsage.InputTokens += usage.InputTokens
	r.totalUsage.OutputTokens += usage.OutputTokens
	r.totalUsage.ReasoningTokens += usage.ReasoningTokens
	r.totalUsage.CacheReadTokens += usage.CacheReadTokens
	r.totalUsage.CacheWriteTokens += usage.CacheWriteTokens
	r.totalCost += stepCost
}

func (r *RunCost) TotalUsage() TokenUsage {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.totalUsage
}

func (r *RunCost) TotalCost() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.totalCost
}

func (r *RunCost) Snapshot() map[string]any {
	r.mu.RLock()
	defer r.mu.RUnlock()

	steps := make([]map[string]any, len(r.steps))
	for i, s := range r.steps {
		steps[i] = map[string]any{
			"step":   s.StepIndex,
			"input":  s.Usage.InputTokens,
			"output": s.Usage.OutputTokens,
			"cost":   s.Cost,
		}
	}

	return map[string]any{
		"run_id": r.runID,
		"model":  r.model,
		"total": map[string]any{
			"input_tokens":       r.totalUsage.InputTokens,
			"output_tokens":      r.totalUsage.OutputTokens,
			"reasoning_tokens":   r.totalUsage.ReasoningTokens,
			"cache_read_tokens":  r.totalUsage.CacheReadTokens,
			"cache_write_tokens": r.totalUsage.CacheWriteTokens,
			"total_tokens":       r.totalUsage.Total(),
			"estimated_cost":     r.totalCost,
		},
		"steps": steps,
	}
}

type CostTrackerStore struct {
	mu     sync.RWMutex
	tracks map[string]*RunCost
}

func NewCostTrackerStore() *CostTrackerStore {
	return &CostTrackerStore{
		tracks: make(map[string]*RunCost),
	}
}

func (s *CostTrackerStore) Create(runID, model string) *RunCost {
	s.mu.Lock()
	defer s.mu.Unlock()
	cost := NewRunCost(runID, model)
	s.tracks[runID] = cost
	return cost
}

func (s *CostTrackerStore) Get(runID string) (*RunCost, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	cost, ok := s.tracks[runID]
	return cost, ok
}

func (s *CostTrackerStore) Delete(runID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.tracks, runID)
}

var globalCostStore = NewCostTrackerStore()
