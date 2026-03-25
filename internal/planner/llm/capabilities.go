package llm

import (
	"sync"
)

type ModelCapability struct {
	ProviderID string
	ModelID    string
	Name       string
	Family     string

	Temperature bool
	Reasoning   bool
	ToolCall    bool
	Attachments bool
	Modalities  []Modality
	Vision      bool
	Audio       bool

	MaxContextTokens int
	MaxOutputTokens  int

	Cost        ModelCost
	Status      ModelStatus
	ReleaseDate string

	RegionPrefix string
	AuthType     string
	ExtraConfig  map[string]any
}

type Modality string

const (
	ModalityText  Modality = "text"
	ModalityImage Modality = "image"
	ModalityVideo Modality = "video"
	ModalityAudio Modality = "audio"
	ModalityPDF   Modality = "pdf"
)

type ModelCost struct {
	InputPer1M     float64
	OutputPer1M    float64
	ReasoningPer1M float64

	CacheReadPer1M  float64
	CacheWritePer1M float64

	ContextOver200K *ModelCost
}

type ModelStatus string

const (
	ModelStatusAlpha      ModelStatus = "alpha"
	ModelStatusBeta       ModelStatus = "beta"
	ModelStatusStable     ModelStatus = "stable"
	ModelStatusDeprecated ModelStatus = "deprecated"
)

type Provider interface {
	ID() string
	GetModel(modelID string) (*ModelCapability, bool)
	ListModels() []*ModelCapability
	GetDefaultModel() string
}

type ModelProviderRegistry struct {
	mu        sync.RWMutex
	providers map[string]*modelProviderImpl
}

type modelProviderImpl struct {
	id           string
	models       map[string]*ModelCapability
	defaultModel string
}

var globalModelRegistry = &ModelProviderRegistry{
	providers: make(map[string]*modelProviderImpl),
}

func GetModelRegistry() *ModelProviderRegistry {
	return globalModelRegistry
}

func (r *ModelProviderRegistry) RegisterProvider(providerID string, provider *modelProviderImpl) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providers[providerID] = provider
}

func (r *ModelProviderRegistry) GetProvider(providerID string) (Provider, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[providerID]
	if !ok {
		return nil, false
	}
	return p, true
}

func (r *ModelProviderRegistry) ListProviders() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.providers))
	for id := range r.providers {
		ids = append(ids, id)
	}
	return ids
}

func (r *ModelProviderRegistry) GetModel(providerID, modelID string) (*ModelCapability, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	p, ok := r.providers[providerID]
	if !ok {
		return nil, false
	}
	m, ok := p.models[modelID]
	return m, ok
}

func (r *ModelProviderRegistry) ListAllModels() []*ModelCapability {
	r.mu.RLock()
	defer r.mu.RUnlock()
	var models []*ModelCapability
	for _, p := range r.providers {
		for _, m := range p.models {
			models = append(models, m)
		}
	}
	return models
}

func (p *modelProviderImpl) ID() string {
	return p.id
}

func (p *modelProviderImpl) GetModel(modelID string) (*ModelCapability, bool) {
	m, ok := p.models[modelID]
	return m, ok
}

func (p *modelProviderImpl) ListModels() []*ModelCapability {
	models := make([]*ModelCapability, 0, len(p.models))
	for _, m := range p.models {
		models = append(models, m)
	}
	return models
}

func (p *modelProviderImpl) GetDefaultModel() string {
	return p.defaultModel
}

func RegisterProviderModels(providerID, defaultModel string, models []*ModelCapability) {
	p := &modelProviderImpl{
		id:           providerID,
		models:       make(map[string]*ModelCapability),
		defaultModel: defaultModel,
	}
	for _, m := range models {
		m.ProviderID = providerID
		p.models[m.ModelID] = m
	}
	GetModelRegistry().RegisterProvider(providerID, p)
}

func NewModelCapability(modelID, name, family string) *ModelCapability {
	return &ModelCapability{
		ModelID:     modelID,
		Name:        name,
		Family:      family,
		Cost:        ModelCost{},
		Status:      ModelStatusStable,
		ExtraConfig: make(map[string]any),
	}
}

func (m *ModelCapability) WithTemperature() *ModelCapability {
	m.Temperature = true
	return m
}

func (m *ModelCapability) WithReasoning() *ModelCapability {
	m.Reasoning = true
	return m
}

func (m *ModelCapability) WithToolCall() *ModelCapability {
	m.ToolCall = true
	return m
}

func (m *ModelCapability) WithAttachments() *ModelCapability {
	m.Attachments = true
	return m
}

func (m *ModelCapability) WithVision() *ModelCapability {
	m.Vision = true
	m.addModality(ModalityImage)
	return m
}

func (m *ModelCapability) WithAudio() *ModelCapability {
	m.Audio = true
	m.addModality(ModalityAudio)
	return m
}

func (m *ModelCapability) WithContextLimit(context, output int) *ModelCapability {
	m.MaxContextTokens = context
	m.MaxOutputTokens = output
	return m
}

func (m *ModelCapability) WithCost(input, output float64) *ModelCapability {
	m.Cost.InputPer1M = input
	m.Cost.OutputPer1M = output
	return m
}

func (m *ModelCapability) WithCacheCost(read, write float64) *ModelCapability {
	m.Cost.CacheReadPer1M = read
	m.Cost.CacheWritePer1M = write
	return m
}

func (m *ModelCapability) WithReasoningCost(reasoning float64) *ModelCapability {
	m.Cost.ReasoningPer1M = reasoning
	return m
}

func (m *ModelCapability) WithStatus(status ModelStatus) *ModelCapability {
	m.Status = status
	return m
}

func (m *ModelCapability) WithReleaseDate(date string) *ModelCapability {
	m.ReleaseDate = date
	return m
}

func (m *ModelCapability) WithRegionPrefix(prefix string) *ModelCapability {
	m.RegionPrefix = prefix
	return m
}

func (m *ModelCapability) addModality(mod Modality) {
	for _, existing := range m.Modalities {
		if existing == mod {
			return
		}
	}
	m.Modalities = append(m.Modalities, mod)
}

func (m *ModelCapability) HasModality(mod Modality) bool {
	for _, existing := range m.Modalities {
		if existing == mod {
			return true
		}
	}
	return false
}

func (m *ModelCapability) SupportsImages() bool {
	return m.HasModality(ModalityImage) || m.Vision
}

func (m *ModelCapability) SupportsAudio() bool {
	return m.HasModality(ModalityAudio) || m.Audio
}

func (m *ModelCapability) SupportsVideo() bool {
	return m.HasModality(ModalityVideo)
}

func (m *ModelCapability) SupportsPDF() bool {
	return m.HasModality(ModalityPDF)
}

func (m *ModelCapability) CalculateCost(inputTokens, outputTokens int) float64 {
	cost := float64(inputTokens) * m.Cost.InputPer1M / 1_000_000
	cost += float64(outputTokens) * m.Cost.OutputPer1M / 1_000_000
	return cost
}

func (m *ModelCapability) IsOver200KContext() bool {
	return m.Cost.ContextOver200K != nil
}

func init() {
	registerBuiltinModels()
}

func registerBuiltinModels() {
	anthropicModels := []*ModelCapability{
		{
			ModelID:     "claude-3-5-sonnet-20241022",
			Name:        "Claude 3.5 Sonnet",
			Family:      "claude-3.5-sonnet",
			Temperature: true, Reasoning: true, ToolCall: true, Attachments: true,
			Vision: true, Audio: false,
			MaxContextTokens: 200000, MaxOutputTokens: 8192,
			Cost:   ModelCost{InputPer1M: 3.00, OutputPer1M: 15.00, ReasoningPer1M: 3.00},
			Status: ModelStatusStable, ReleaseDate: "2024-10-22",
		},
		{
			ModelID:     "claude-3-5-haiku-20241022",
			Name:        "Claude 3.5 Haiku",
			Family:      "claude-3.5-haiku",
			Temperature: true, ToolCall: true, Attachments: true,
			MaxContextTokens: 200000, MaxOutputTokens: 8192,
			Cost:   ModelCost{InputPer1M: 1.00, OutputPer1M: 5.00},
			Status: ModelStatusStable, ReleaseDate: "2024-10-22",
		},
		{
			ModelID:     "claude-3-opus",
			Name:        "Claude 3 Opus",
			Family:      "claude-3-opus",
			Temperature: true, Reasoning: true, ToolCall: true, Attachments: true,
			Vision:           true,
			MaxContextTokens: 200000, MaxOutputTokens: 4096,
			Cost:   ModelCost{InputPer1M: 15.00, OutputPer1M: 75.00, ReasoningPer1M: 15.00},
			Status: ModelStatusStable, ReleaseDate: "2024-02-29",
		},
		{
			ModelID:     "claude-3-haiku",
			Name:        "Claude 3 Haiku",
			Family:      "claude-3-haiku",
			Temperature: true, ToolCall: true, Attachments: true,
			MaxContextTokens: 200000, MaxOutputTokens: 4096,
			Cost:   ModelCost{InputPer1M: 0.25, OutputPer1M: 1.25},
			Status: ModelStatusStable, ReleaseDate: "2024-02-29",
		},
		{
			ModelID:     "claude-sonnet-4-5",
			Name:        "Claude Sonnet 4",
			Family:      "claude-sonnet",
			Temperature: true, Reasoning: true, ToolCall: true, Attachments: true,
			Vision:           true,
			MaxContextTokens: 200000, MaxOutputTokens: 8192,
			Cost:   ModelCost{InputPer1M: 3.00, OutputPer1M: 15.00, ReasoningPer1M: 3.00},
			Status: ModelStatusStable, ReleaseDate: "2024-07-15",
		},
	}

	openaiModels := []*ModelCapability{
		{
			ModelID:     "gpt-4o",
			Name:        "GPT-4o",
			Family:      "gpt-4o",
			Temperature: true, ToolCall: true, Attachments: true,
			Vision:           true,
			MaxContextTokens: 128000, MaxOutputTokens: 16384,
			Cost:   ModelCost{InputPer1M: 2.50, OutputPer1M: 10.00},
			Status: ModelStatusStable, ReleaseDate: "2024-05-13",
		},
		{
			ModelID:     "gpt-4o-mini",
			Name:        "GPT-4o Mini",
			Family:      "gpt-4o-mini",
			Temperature: true, ToolCall: true, Attachments: true,
			MaxContextTokens: 128000, MaxOutputTokens: 16384,
			Cost:   ModelCost{InputPer1M: 0.15, OutputPer1M: 0.60},
			Status: ModelStatusStable, ReleaseDate: "2024-07-18",
		},
		{
			ModelID:     "gpt-4-turbo",
			Name:        "GPT-4 Turbo",
			Family:      "gpt-4-turbo",
			Temperature: true, ToolCall: true, Attachments: true,
			Vision:           true,
			MaxContextTokens: 128000, MaxOutputTokens: 4096,
			Cost:   ModelCost{InputPer1M: 10.00, OutputPer1M: 30.00},
			Status: ModelStatusStable, ReleaseDate: "2024-04-09",
		},
		{
			ModelID:          "o1-preview",
			Name:             "o1 Preview",
			Family:           "o1",
			Reasoning:        true,
			MaxContextTokens: 128000, MaxOutputTokens: 32768,
			Cost:   ModelCost{InputPer1M: 15.00, OutputPer1M: 60.00, ReasoningPer1M: 60.00},
			Status: ModelStatusBeta, ReleaseDate: "2024-09-12",
		},
		{
			ModelID:          "o1-mini",
			Name:             "o1 Mini",
			Family:           "o1",
			Reasoning:        true,
			MaxContextTokens: 128000, MaxOutputTokens: 65536,
			Cost:   ModelCost{InputPer1M: 3.00, OutputPer1M: 12.00, ReasoningPer1M: 12.00},
			Status: ModelStatusBeta, ReleaseDate: "2024-09-12",
		},
	}

	googleModels := []*ModelCapability{
		{
			ModelID:     "gemini-2.0-flash",
			Name:        "Gemini 2.0 Flash",
			Family:      "gemini-2.0-flash",
			Temperature: true, ToolCall: true, Attachments: true,
			Vision: true, Audio: true,
			MaxContextTokens: 1000000, MaxOutputTokens: 8192,
			Cost:   ModelCost{InputPer1M: 0.10, OutputPer1M: 0.40, CacheReadPer1M: 0.01},
			Status: ModelStatusStable, ReleaseDate: "2024-12-11",
		},
		{
			ModelID:     "gemini-1.5-pro",
			Name:        "Gemini 1.5 Pro",
			Family:      "gemini-1.5-pro",
			Temperature: true, ToolCall: true, Attachments: true,
			Vision:           true,
			MaxContextTokens: 2000000, MaxOutputTokens: 8192,
			Cost:   ModelCost{InputPer1M: 1.25, OutputPer1M: 5.00, CacheReadPer1M: 0.10},
			Status: ModelStatusStable, ReleaseDate: "2024-05-24",
		},
		{
			ModelID:     "gemini-1.5-flash",
			Name:        "Gemini 1.5 Flash",
			Family:      "gemini-1.5-flash",
			Temperature: true, ToolCall: true, Attachments: true,
			Vision:           true,
			MaxContextTokens: 1000000, MaxOutputTokens: 8192,
			Cost:   ModelCost{InputPer1M: 0.075, OutputPer1M: 0.30, CacheReadPer1M: 0.01},
			Status: ModelStatusStable, ReleaseDate: "2024-08-01",
		},
		{
			ModelID:     "gemini-1.5-flash-8b",
			Name:        "Gemini 1.5 Flash 8B",
			Family:      "gemini-1.5-flash",
			Temperature: true, ToolCall: true, Attachments: true,
			MaxContextTokens: 1000000, MaxOutputTokens: 8192,
			Cost:   ModelCost{InputPer1M: 0.0375, OutputPer1M: 0.15, CacheReadPer1M: 0.005},
			Status: ModelStatusStable, ReleaseDate: "2024-08-01",
		},
	}

	deepseekModels := []*ModelCapability{
		{
			ModelID:     "deepseek-chat",
			Name:        "DeepSeek Chat",
			Family:      "deepseek-chat",
			Temperature: true, ToolCall: true,
			MaxContextTokens: 128000, MaxOutputTokens: 8192,
			Cost:   ModelCost{InputPer1M: 0.14, OutputPer1M: 2.80},
			Status: ModelStatusStable, ReleaseDate: "2024-01-25",
		},
		{
			ModelID:          "deepseek-reasoner",
			Name:             "DeepSeek R1",
			Family:           "deepseek-reasoner",
			Reasoning:        true,
			MaxContextTokens: 128000, MaxOutputTokens: 8192,
			Cost:   ModelCost{InputPer1M: 0.14, OutputPer1M: 2.80, ReasoningPer1M: 2.80},
			Status: ModelStatusBeta, ReleaseDate: "2025-01-20",
		},
	}

	RegisterProviderModels("anthropic", "claude-3-5-sonnet-20241022", anthropicModels)
	RegisterProviderModels("openai", "gpt-4o", openaiModels)
	RegisterProviderModels("google", "gemini-2.0-flash", googleModels)
	RegisterProviderModels("deepseek", "deepseek-chat", deepseekModels)
}

func GetModelCapability(providerID, modelID string) *ModelCapability {
	if cap, ok := GetModelRegistry().GetModel(providerID, modelID); ok {
		return cap
	}
	for _, p := range GetModelRegistry().ListProviders() {
		prov, _ := GetModelRegistry().GetProvider(p)
		if models := prov.ListModels(); models != nil {
			for _, m := range models {
				if m.Family != "" && len(modelID) >= len(m.Family) && modelID[:len(m.Family)] == m.Family {
					return m
				}
			}
		}
	}
	return nil
}

func ModelSupportsToolCall(providerID, modelID string) bool {
	cap := GetModelCapability(providerID, modelID)
	return cap != nil && cap.ToolCall
}

func ModelSupportsReasoning(providerID, modelID string) bool {
	cap := GetModelCapability(providerID, modelID)
	return cap != nil && cap.Reasoning
}

func ModelSupportsVision(providerID, modelID string) bool {
	cap := GetModelCapability(providerID, modelID)
	return cap != nil && cap.SupportsImages()
}

func ModelGetMaxContext(providerID, modelID string) int {
	cap := GetModelCapability(providerID, modelID)
	if cap == nil {
		return 128000
	}
	return cap.MaxContextTokens
}
