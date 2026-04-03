package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/zetatez/morpheus/pkg/sdk"
)

func init() {
	RegisterProvider("openai", NewOpenAIProvider)
	RegisterProvider("minimax", NewMiniMaxProvider)
	RegisterProvider("glm", NewGLMProvider)
	RegisterProvider("deepseek", NewDeepSeekProvider)
	RegisterProvider("gemini", NewGeminiProvider)
	RegisterProvider("anthropic", NewAnthropicProvider)
	RegisterProvider("openrouter", NewOpenRouterProvider)
	RegisterProvider("azure", NewAzureProvider)
	RegisterProvider("ollama", NewOllamaProvider)
	RegisterProvider("lmstudio", NewLMStudioProvider)
	RegisterProvider("groq", NewGroqProvider)
	RegisterProvider("mistral", NewMistralProvider)
	RegisterProvider("cohere", NewCohereProvider)
	RegisterProvider("togetherai", NewTogetherAIProvider)
	RegisterProvider("perplexity", NewPerplexityProvider)
	RegisterProvider("openai-compatible", NewOpenAICompatibleProvider)
}

type OpenAIProvider struct {
	*BasePlanner
}

func NewOpenAIProvider(config PlannerProviderConfig) (Planner, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("openai provider requires api_key")
	}
	model := config.Model
	if model == "" {
		model = "gpt-4o"
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://api.openai.com/v1/chat/completions"
	}
	return &OpenAIProvider{
		BasePlanner: NewBasePlanner(config.APIKey, model, config.Temperature, endpoint, config.ExtraHeaders),
	}, nil
}

func (p *OpenAIProvider) ID() string { return "openai" }

func (p *OpenAIProvider) Capabilities() []string { return []string{"fs", "cmd", "search", "edit"} }

type MiniMaxProvider struct {
	apiKey     string
	model      string
	temp       float64
	endpoint   string
	httpClient *http.Client
}

func NewMiniMaxProvider(config PlannerProviderConfig) (Planner, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("minimax provider requires api_key")
	}
	model := config.Model
	if model == "" {
		model = "MiniMax-M2.7"
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://api.minimaxi.com"
	}
	return &MiniMaxProvider{
		apiKey:     config.APIKey,
		model:      model,
		temp:       config.Temperature,
		endpoint:   endpoint,
		httpClient: &http.Client{},
	}, nil
}

func (p *MiniMaxProvider) ID() string { return "minimax" }

func (p *MiniMaxProvider) Plan(ctx context.Context, req sdk.PlanRequest) (sdk.Plan, error) {
	userPrompt := req.Prompt
	if len(req.Context) > 0 {
		var ctxLines []string
		for _, c := range req.Context {
			ctxLines = append(ctxLines, c.Content)
		}
		userPrompt = "Context:\n" + strings.Join(ctxLines, "\n") + "\n\nRequest: " + userPrompt
	}

	systemPrompt := p.GetSystemPrompt()

	fmt.Fprintf(os.Stderr, "DEBUG Plan: model=%q isAnthropic=%v\n", p.model, isMiniMaxAnthropicModel(p.model))

	if isMiniMaxAnthropicModel(p.model) {
		return p.planAnthropic(ctx, systemPrompt, userPrompt)
	}
	return p.planOpenAI(ctx, systemPrompt, userPrompt)
}

func isMiniMaxAnthropicModel(model string) bool {
	switch model {
	case "MiniMax-M2.7", "MiniMax-M2.7-highspeed", "MiniMax-M2.5", "MiniMax-M2.5-highspeed",
		"MiniMax-M2.1", "MiniMax-M2.1-highspeed", "MiniMax-M2":
		return true
	}
	return false
}

func (p *MiniMaxProvider) planAnthropic(ctx context.Context, systemPrompt, userPrompt string) (sdk.Plan, error) {
	payload := map[string]any{
		"model": p.model,
		"messages": []map[string]any{
			{"role": "user", "content": systemPrompt + "\n\n" + userPrompt},
		},
		"max_tokens":  2048,
		"temperature": p.temp,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return sdk.Plan{}, err
	}

	url := p.endpoint + "/anthropic/v1/messages"
	fmt.Printf("DEBUG: URL=%s body=%s\n", url, string(body))

	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(body))
	if err != nil {
		return sdk.Plan{}, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return sdk.Plan{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return sdk.Plan{}, err
	}

	if resp.StatusCode != http.StatusOK {
		return sdk.Plan{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var raw struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}

	if err := json.Unmarshal(respBody, &raw); err != nil {
		return sdk.Plan{}, err
	}

	if raw.Error.Type != "" {
		return sdk.Plan{}, fmt.Errorf("minimax error: %s", raw.Error.Message)
	}

	if len(raw.Content) == 0 {
		return sdk.Plan{}, fmt.Errorf("no response content")
	}

	var content string
	for _, block := range raw.Content {
		if block.Type == "text" {
			content = block.Text
			break
		}
	}
	return p.parsePlanResponse(content)
}

func (p *MiniMaxProvider) planOpenAI(ctx context.Context, systemPrompt, userPrompt string) (sdk.Plan, error) {
	payload := map[string]any{
		"model":       p.model,
		"messages":    []map[string]string{{"role": "system", "content": systemPrompt}, {"role": "user", "content": userPrompt}},
		"temperature": p.temp,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return sdk.Plan{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.endpoint+"/v1/text/chatcompletion_v2", bytes.NewReader(body))
	if err != nil {
		return sdk.Plan{}, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return sdk.Plan{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return sdk.Plan{}, err
	}

	if resp.StatusCode != http.StatusOK {
		return sdk.Plan{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var raw struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
				Role    string `json:"role"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Error struct {
			Code    int    `json:"status_code"`
			Message string `json:"status_msg"`
		} `json:"base_resp"`
	}

	if err := json.Unmarshal(respBody, &raw); err != nil {
		return sdk.Plan{}, err
	}

	if raw.Error.Code != 0 {
		return sdk.Plan{}, fmt.Errorf("minimax error (%d): %s", raw.Error.Code, raw.Error.Message)
	}

	if len(raw.Choices) == 0 {
		return sdk.Plan{}, fmt.Errorf("no response choices")
	}

	content := raw.Choices[0].Message.Content
	return p.parsePlanResponse(content)
}

func (p *MiniMaxProvider) parseResponse(body []byte) (string, error) {
	var result struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	return result.Content, nil
}

func (p *MiniMaxProvider) GetSystemPrompt() string {
	return `You are Morpheus, an autonomous coding assistant. Plan tasks efficiently with minimal user interaction.

## Core Principles
- Think independently; make decisions without asking
- Choose safe defaults and proceed
- Keep responses brief and direct

## Code Writing (see coding.md for details)
- Write minimum viable code first
- Prefer simple over clever
- Don't optimize prematurely unless asked

## Workflow
1. Understand the task fully before planning
2. Use the right tool for each operation
3. Execute efficiently - 1-3 steps per task
4. Output results directly

## Tool Selection
- fs.read/fs.write/fs.edit: File operations
- fs.glob/fs.grep: Search files
- lsp.query: Code navigation
- cmd.exec: Shell commands

## Output Format (valid JSON only):
{"summary": "1-2 line summary", "steps": [{"description": "action", "tool": "name", "inputs": {"key": "value"}}], "risks": []}`
}

func (p *MiniMaxProvider) parsePlanResponse(content string) (sdk.Plan, error) {
	var plan struct {
		Summary string `json:"summary"`
		Steps   []struct {
			Description string         `json:"description"`
			Tool        string         `json:"tool"`
			Inputs      map[string]any `json:"inputs"`
		} `json:"steps"`
		Risks []string `json:"risks"`
	}

	if err := json.Unmarshal([]byte(content), &plan); err != nil {
		return sdk.Plan{}, fmt.Errorf("failed to parse plan response: %v", err)
	}

	var steps []sdk.PlanStep
	for _, s := range plan.Steps {
		steps = append(steps, sdk.PlanStep{
			Description: s.Description,
			Tool:        s.Tool,
			Inputs:      s.Inputs,
		})
	}

	return sdk.Plan{
		Summary: plan.Summary,
		Steps:   steps,
		Risks:   plan.Risks,
	}, nil
}

func (p *MiniMaxProvider) Capabilities() []string { return []string{"fs", "cmd", "search", "edit"} }

type GLMProvider struct {
	*BasePlanner
}

func NewGLMProvider(config PlannerProviderConfig) (Planner, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("glm provider requires api_key")
	}
	model := config.Model
	if model == "" {
		model = "glm-4"
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://open.bigmodel.cn/api/paas/v4/chat/completions"
	}
	return &GLMProvider{
		BasePlanner: NewBasePlanner(config.APIKey, model, config.Temperature, endpoint, config.ExtraHeaders),
	}, nil
}

func (p *GLMProvider) ID() string { return "glm" }

func (p *GLMProvider) Capabilities() []string { return []string{"fs", "cmd", "search", "edit"} }

type DeepSeekProvider struct {
	*BasePlanner
}

func NewDeepSeekProvider(config PlannerProviderConfig) (Planner, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("deepseek provider requires api_key")
	}
	model := config.Model
	if model == "" {
		model = "deepseek-chat"
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://api.deepseek.com/v1/chat/completions"
	}
	return &DeepSeekProvider{
		BasePlanner: NewBasePlanner(config.APIKey, model, config.Temperature, endpoint, config.ExtraHeaders),
	}, nil
}

func (p *DeepSeekProvider) ID() string { return "deepseek" }

func (p *DeepSeekProvider) Capabilities() []string { return []string{"fs", "cmd", "search", "edit"} }

type GeminiProvider struct {
	*BasePlanner
}

func NewGeminiProvider(config PlannerProviderConfig) (Planner, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("gemini provider requires api_key")
	}
	model := config.Model
	if model == "" {
		model = "gemini-2.0-flash"
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://generativelanguage.googleapis.com/v1beta/models/" + model + ":generateContent"
	}
	extraHeaders := make(map[string]string)
	extraHeaders["x-goog-api-key"] = config.APIKey
	for k, v := range config.ExtraHeaders {
		extraHeaders[k] = v
	}
	p := &GeminiProvider{
		BasePlanner: NewBasePlanner(config.APIKey, model, config.Temperature, endpoint, extraHeaders),
	}
	return p, nil
}

func (p *GeminiProvider) ID() string { return "gemini" }

func (p *GeminiProvider) Capabilities() []string { return []string{"fs", "cmd", "search", "edit"} }

func (p *GeminiProvider) Plan(ctx context.Context, req sdk.PlanRequest) (sdk.Plan, error) {
	userPrompt := req.Prompt
	if len(req.Context) > 0 {
		var ctxLines []string
		for _, c := range req.Context {
			ctxLines = append(ctxLines, c.Content)
		}
		userPrompt = "Context:\n" + strings.Join(ctxLines, "\n") + "\n\nRequest: " + userPrompt
	}

	systemPrompt := p.GetSystemPrompt()

	payload := map[string]any{
		"contents": []map[string]any{
			{"role": "user", "parts": []map[string]any{{"text": systemPrompt + "\n\n" + userPrompt}}},
		},
		"safety_settings": []map[string]any{
			{"category": "HARM_CATEGORY_DANGEROUS_CONTENT", "threshold": "BLOCK_NONE"},
			{"category": "HARM_CATEGORY_HARASSMENT", "threshold": "BLOCK_NONE"},
			{"category": "HARM_CATEGORY_HATE_SPEECH", "threshold": "BLOCK_NONE"},
			{"category": "HARM_CATEGORY_SEXUALLY_EXPLICIT", "threshold": "BLOCK_NONE"},
		},
		"generation_config": map[string]any{
			"temperature": p.temp,
			"topP":        0.95,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return sdk.Plan{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.endpoint, strings.NewReader(string(body)))
	if err != nil {
		return sdk.Plan{}, err
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return sdk.Plan{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return sdk.Plan{}, err
	}

	if resp.StatusCode != http.StatusOK {
		return sdk.Plan{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	content, err := p.parseResponse(respBody)
	if err != nil {
		return sdk.Plan{}, err
	}

	return p.parsePlanResponse(content)
}

func (p *GeminiProvider) parseResponse(body []byte) (string, error) {
	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if len(result.Candidates) > 0 && len(result.Candidates[0].Content.Parts) > 0 {
		return result.Candidates[0].Content.Parts[0].Text, nil
	}
	return "", fmt.Errorf("no content in response")
}

type AnthropicProvider struct {
	*BasePlanner
}

func NewAnthropicProvider(config PlannerProviderConfig) (Planner, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("anthropic provider requires api_key")
	}
	model := config.Model
	if model == "" {
		model = "claude-sonnet-4-5"
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://api.anthropic.com/v1/messages"
	}
	headers := map[string]string{
		"x-api-key":         config.APIKey,
		"anthropic-version": "2023-06-01",
	}
	for k, v := range config.ExtraHeaders {
		headers[k] = v
	}
	return &AnthropicProvider{
		BasePlanner: NewBasePlanner(config.APIKey, model, config.Temperature, endpoint, headers),
	}, nil
}

func (p *AnthropicProvider) ID() string { return "anthropic" }

func (p *AnthropicProvider) Capabilities() []string { return []string{"fs", "cmd", "search", "edit"} }

func (p *AnthropicProvider) Plan(ctx context.Context, req sdk.PlanRequest) (sdk.Plan, error) {
	userPrompt := req.Prompt
	if len(req.Context) > 0 {
		var ctxLines []string
		for _, c := range req.Context {
			ctxLines = append(ctxLines, c.Content)
		}
		userPrompt = "Context:\n" + strings.Join(ctxLines, "\n") + "\n\nRequest: " + userPrompt
	}

	systemPrompt := p.GetSystemPrompt()

	payload := map[string]any{
		"model": p.model,
		"messages": []map[string]any{
			{"role": "user", "content": systemPrompt + "\n\n" + userPrompt},
		},
		"temperature": p.temp,
		"max_tokens":  4096,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return sdk.Plan{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.endpoint, strings.NewReader(string(body)))
	if err != nil {
		return sdk.Plan{}, err
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")
	for k, v := range p.extraHeaders {
		httpReq.Header.Set(k, v)
	}

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return sdk.Plan{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return sdk.Plan{}, err
	}

	if resp.StatusCode != http.StatusOK {
		return sdk.Plan{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	content, err := p.parseResponse(respBody)
	if err != nil {
		return sdk.Plan{}, err
	}

	return p.parsePlanResponse(content)
}

func (p *AnthropicProvider) parseResponse(body []byte) (string, error) {
	var result struct {
		Content []struct {
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	if len(result.Content) > 0 {
		return result.Content[0].Text, nil
	}
	return "", fmt.Errorf("no content in response")
}

type OpenRouterProvider struct {
	*BasePlanner
}

func NewOpenRouterProvider(config PlannerProviderConfig) (Planner, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("openrouter provider requires api_key")
	}
	model := config.Model
	if model == "" {
		model = "openai/gpt-4o"
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://openrouter.ai/api/v1/chat/completions"
	}
	headers := map[string]string{
		"HTTP-Referer": "https://morpheus.ai/",
		"X-Title":      "Morpheus",
	}
	for k, v := range config.ExtraHeaders {
		headers[k] = v
	}
	return &OpenRouterProvider{
		BasePlanner: NewBasePlanner(config.APIKey, model, config.Temperature, endpoint, headers),
	}, nil
}

func (p *OpenRouterProvider) ID() string { return "openrouter" }

func (p *OpenRouterProvider) Capabilities() []string { return []string{"fs", "cmd", "search", "edit"} }

type AzureProvider struct {
	*BasePlanner
	apiVersion string
}

func NewAzureProvider(config PlannerProviderConfig) (Planner, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("azure provider requires api_key")
	}
	model := config.Model
	if model == "" {
		model = "gpt-4o"
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		return nil, fmt.Errorf("azure provider requires endpoint configuration")
	}
	apiVersion := "2024-02-01"
	if strings.Contains(endpoint, "api-version=") {
		for _, pair := range strings.Split(endpoint, "?") {
			if strings.HasPrefix(pair, "api-version=") {
				apiVersion = strings.TrimPrefix(pair, "api-version=")
				break
			}
		}
	}
	headers := map[string]string{
		"api-key": config.APIKey,
	}
	for k, v := range config.ExtraHeaders {
		headers[k] = v
	}
	return &AzureProvider{
		BasePlanner: NewBasePlanner(config.APIKey, model, config.Temperature, endpoint, headers),
		apiVersion:  apiVersion,
	}, nil
}

func (p *AzureProvider) ID() string { return "azure" }

func (p *AzureProvider) Capabilities() []string { return []string{"fs", "cmd", "search", "edit"} }

type OllamaProvider struct {
	*BasePlanner
}

func NewOllamaProvider(config PlannerProviderConfig) (Planner, error) {
	model := config.Model
	if model == "" {
		model = "llama3.2"
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "http://localhost:11434/api/chat"
	}
	return &OllamaProvider{
		BasePlanner: NewBasePlanner("", model, config.Temperature, endpoint, config.ExtraHeaders),
	}, nil
}

func (p *OllamaProvider) ID() string { return "ollama" }

func (p *OllamaProvider) Capabilities() []string { return []string{"fs", "cmd", "search", "edit"} }

func (p *OllamaProvider) Plan(ctx context.Context, req sdk.PlanRequest) (sdk.Plan, error) {
	userPrompt := req.Prompt
	if len(req.Context) > 0 {
		var ctxLines []string
		for _, c := range req.Context {
			ctxLines = append(ctxLines, c.Content)
		}
		userPrompt = "Context:\n" + strings.Join(ctxLines, "\n") + "\n\nRequest: " + userPrompt
	}

	systemPrompt := p.GetSystemPrompt()

	payload := map[string]any{
		"model": p.model,
		"messages": []map[string]any{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"stream": false,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return sdk.Plan{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.endpoint, strings.NewReader(string(body)))
	if err != nil {
		return sdk.Plan{}, err
	}

	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return sdk.Plan{}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return sdk.Plan{}, err
	}

	if resp.StatusCode != http.StatusOK {
		return sdk.Plan{}, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	content, err := p.parseResponse(respBody)
	if err != nil {
		return sdk.Plan{}, err
	}

	return p.parsePlanResponse(content)
}

func (p *OllamaProvider) parseResponse(body []byte) (string, error) {
	var result struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", err
	}
	return result.Message.Content, nil
}

type LMStudioProvider struct {
	*BasePlanner
}

func NewLMStudioProvider(config PlannerProviderConfig) (Planner, error) {
	model := config.Model
	if model == "" {
		model = "local-model"
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "http://localhost:1234/v1/chat/completions"
	}
	return &LMStudioProvider{
		BasePlanner: NewBasePlanner("", model, config.Temperature, endpoint, config.ExtraHeaders),
	}, nil
}

func (p *LMStudioProvider) ID() string { return "lmstudio" }

func (p *LMStudioProvider) Capabilities() []string { return []string{"fs", "cmd", "search", "edit"} }

type GroqProvider struct {
	*BasePlanner
}

func NewGroqProvider(config PlannerProviderConfig) (Planner, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("groq provider requires api_key")
	}
	model := config.Model
	if model == "" {
		model = "mixtral-8x7b-32768"
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://api.groq.com/openai/v1/chat/completions"
	}
	headers := map[string]string{
		"Authorization": "Bearer " + config.APIKey,
	}
	for k, v := range config.ExtraHeaders {
		headers[k] = v
	}
	return &GroqProvider{
		BasePlanner: NewBasePlanner(config.APIKey, model, config.Temperature, endpoint, headers),
	}, nil
}

func (p *GroqProvider) ID() string { return "groq" }

func (p *GroqProvider) Capabilities() []string { return []string{"fs", "cmd", "search", "edit"} }

type MistralProvider struct {
	*BasePlanner
}

func NewMistralProvider(config PlannerProviderConfig) (Planner, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("mistral provider requires api_key")
	}
	model := config.Model
	if model == "" {
		model = "mistral-large-latest"
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://api.mistral.ai/v1/chat/completions"
	}
	return &MistralProvider{
		BasePlanner: NewBasePlanner(config.APIKey, model, config.Temperature, endpoint, config.ExtraHeaders),
	}, nil
}

func (p *MistralProvider) ID() string { return "mistral" }

func (p *MistralProvider) Capabilities() []string { return []string{"fs", "cmd", "search", "edit"} }

type CohereProvider struct {
	*BasePlanner
}

func NewCohereProvider(config PlannerProviderConfig) (Planner, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("cohere provider requires api_key")
	}
	model := config.Model
	if model == "" {
		model = "command-r-plus"
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://api.cohere.ai/v2/chat"
	}
	headers := map[string]string{
		"Authorization": "Bearer " + config.APIKey,
		"Content-Type":  "application/json",
	}
	for k, v := range config.ExtraHeaders {
		headers[k] = v
	}
	return &CohereProvider{
		BasePlanner: NewBasePlanner(config.APIKey, model, config.Temperature, endpoint, headers),
	}, nil
}

func (p *CohereProvider) ID() string { return "cohere" }

func (p *CohereProvider) Capabilities() []string { return []string{"fs", "cmd", "search", "edit"} }

type TogetherAIProvider struct {
	*BasePlanner
}

func NewTogetherAIProvider(config PlannerProviderConfig) (Planner, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("togetherai provider requires api_key")
	}
	model := config.Model
	if model == "" {
		model = "meta-llama/Llama-3.2-90B-Vision-Instruct-Turbo"
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://api.together.ai/v1/chat/completions"
	}
	return &TogetherAIProvider{
		BasePlanner: NewBasePlanner(config.APIKey, model, config.Temperature, endpoint, config.ExtraHeaders),
	}, nil
}

func (p *TogetherAIProvider) ID() string { return "togetherai" }

func (p *TogetherAIProvider) Capabilities() []string { return []string{"fs", "cmd", "search", "edit"} }

type PerplexityProvider struct {
	*BasePlanner
}

func NewPerplexityProvider(config PlannerProviderConfig) (Planner, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("perplexity provider requires api_key")
	}
	model := config.Model
	if model == "" {
		model = "llama-3.1-sonar-large-128k-online"
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		endpoint = "https://api.perplexity.ai/chat/completions"
	}
	return &PerplexityProvider{
		BasePlanner: NewBasePlanner(config.APIKey, model, config.Temperature, endpoint, config.ExtraHeaders),
	}, nil
}

func (p *PerplexityProvider) ID() string { return "perplexity" }

func (p *PerplexityProvider) Capabilities() []string { return []string{"fs", "cmd", "search", "edit"} }

type OpenAICompatibleProvider struct {
	*BasePlanner
}

func NewOpenAICompatibleProvider(config PlannerProviderConfig) (Planner, error) {
	if config.APIKey == "" {
		return nil, fmt.Errorf("openai-compatible provider requires api_key")
	}
	model := config.Model
	if model == "" {
		model = "default"
	}
	endpoint := config.Endpoint
	if endpoint == "" {
		return nil, fmt.Errorf("openai-compatible provider requires endpoint configuration")
	}
	return &OpenAICompatibleProvider{
		BasePlanner: NewBasePlanner(config.APIKey, model, config.Temperature, endpoint, config.ExtraHeaders),
	}, nil
}

func (p *OpenAICompatibleProvider) ID() string { return "openai-compatible" }

func (p *OpenAICompatibleProvider) Capabilities() []string {
	return []string{"fs", "cmd", "search", "edit"}
}
