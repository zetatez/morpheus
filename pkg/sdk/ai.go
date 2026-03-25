package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

type ToolCall struct {
	ID        string
	Name      string
	Arguments map[string]any
	Input     map[string]any
}

type ToolDef struct {
	Name        string
	Description string
	Schema      map[string]any
}

type ToolCallRepair func(failed ToolCall, err error) (ToolCall, error)

type ToolConfig struct {
	StrictSchema   bool
	RepairToolCall ToolCallRepair
	NameNormalizer func(string) string
	AllowedTools   []string
	DisabledTools  map[string]bool
}

func DefaultToolConfig() *ToolConfig {
	return &ToolConfig{
		NameNormalizer: defaultNormalizeToolName,
		DisabledTools:  make(map[string]bool),
	}
}

func defaultNormalizeToolName(name string) string {
	clean := strings.ReplaceAll(strings.ReplaceAll(name, "\n", "_"), "\r", "_")
	clean = strings.ReplaceAll(clean, " ", "_")
	clean = strings.ToLower(clean)
	return clean
}

var toolNamePattern = regexp.MustCompile(`[^a-z0-9_]+`)

func NormalizeToolName(name string) string {
	if name == "" {
		return ""
	}
	clean := strings.ReplaceAll(strings.ReplaceAll(name, "\n", "_"), "\r", "_")
	clean = strings.ReplaceAll(clean, " ", "_")
	clean = strings.ReplaceAll(clean, "\t", "_")
	clean = strings.ToLower(clean)
	clean = toolNamePattern.ReplaceAllString(clean, "_")
	clean = strings.Trim(clean, "_")
	if clean == "" {
		return strings.ToLower(name)
	}
	return clean
}

func ToSnakeCase(name string) string {
	if name == "" {
		return ""
	}
	var result strings.Builder
	for i, r := range name {
		if i > 0 && r >= 'A' && r <= 'Z' {
			prev := name[i-1]
			if prev >= 'a' && prev <= 'z' || prev >= 'A' && prev <= 'Z' && (i+1 >= len(name) || name[i+1] >= 'a' && name[i+1] <= 'z') {
				result.WriteRune('_')
			}
		}
		result.WriteRune(r)
	}
	return strings.ToLower(result.String())
}

func RepairToolName(name string, registry func(string) (any, bool)) (string, any, bool) {
	if name == "" {
		return "", nil, false
	}
	if tool, ok := registry(name); ok {
		return name, tool, true
	}
	normalized := NormalizeToolName(name)
	if normalized != name {
		if tool, ok := registry(normalized); ok {
			return normalized, tool, true
		}
	}
	snakeCase := ToSnakeCase(name)
	if snakeCase != name && snakeCase != normalized {
		if tool, ok := registry(snakeCase); ok {
			return snakeCase, tool, true
		}
	}
	lower := strings.ToLower(name)
	if lower != name && lower != normalized && lower != snakeCase {
		if tool, ok := registry(lower); ok {
			return lower, tool, true
		}
	}
	return name, nil, false
}

func (tc *ToolConfig) NormalizeToolName(name string) string {
	if tc.NameNormalizer != nil {
		return tc.NameNormalizer(name)
	}
	return defaultNormalizeToolName(name)
}

func (tc *ToolConfig) RepairFailedToolCall(failed ToolCall, err error) (ToolCall, error) {
	if tc.RepairToolCall != nil {
		return tc.RepairToolCall(failed, err)
	}

	name := tc.NormalizeToolName(failed.Name)
	failed.Name = name
	return failed, nil
}

func (tc *ToolConfig) IsToolAllowed(name string) bool {
	if tc.DisabledTools[name] {
		return false
	}
	if len(tc.AllowedTools) > 0 {
		for _, allowed := range tc.AllowedTools {
			if allowed == name {
				return true
			}
		}
		return false
	}
	return true
}

func ToolSpecToDef(spec ToolSpec) ToolDef {
	return ToolDef{
		Name:        spec.Name(),
		Description: spec.Describe(),
		Schema:      spec.Schema(),
	}
}

func ValidateToolInput(schema map[string]any, input map[string]any) error {
	if schema == nil {
		return nil
	}

	properties, ok := schema["properties"].(map[string]any)
	if !ok {
		return nil
	}

	required, _ := schema["required"].([]any)
	requiredMap := make(map[string]bool)
	for _, r := range required {
		if s, ok := r.(string); ok {
			requiredMap[s] = true
		}
	}

	for key := range requiredMap {
		if _, exists := input[key]; !exists {
			return fmt.Errorf("missing required field: %s", key)
		}
	}

	for key, value := range input {
		prop, exists := properties[key]
		if !exists {
			continue
		}
		if propMap, ok := prop.(map[string]any); ok {
			if err := validateType(propMap, value, key); err != nil {
				return err
			}
		}
	}

	return nil
}

func validateType(prop map[string]any, value any, fieldName string) error {
	expectedType, _ := prop["type"].(string)

	switch expectedType {
	case "string":
		if _, ok := value.(string); !ok {
			return fmt.Errorf("field %s must be string, got %T", fieldName, value)
		}
	case "number", "integer":
		switch value.(type) {
		case float64, int, int64:
		default:
			return fmt.Errorf("field %s must be number, got %T", fieldName, value)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("field %s must be boolean, got %T", fieldName, value)
		}
	case "array":
		if _, ok := value.([]any); !ok {
			return fmt.Errorf("field %s must be array, got %T", fieldName, value)
		}
	case "object":
		if _, ok := value.(map[string]any); !ok {
			return fmt.Errorf("field %s must be object, got %T", fieldName, value)
		}
	}

	return nil
}

func ParseTextToolCalls(content string, regex string) []ToolCall {
	if regex == "" {
		regex = `\{[^}]*tool_calls[^}]*\}`
	}

	re := regexp.MustCompile(regex)
	matches := re.FindStringSubmatch(content)
	if len(matches) == 0 {
		return nil
	}

	var raw struct {
		ToolCalls []struct {
			ID        string          `json:"id"`
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"tool_calls"`
	}

	if err := json.Unmarshal([]byte(matches[0]), &raw); err != nil {
		return nil
	}

	var calls []ToolCall
	for i, tc := range raw.ToolCalls {
		var args map[string]any
		if err := json.Unmarshal(tc.Arguments, &args); err != nil {
			args = map[string]any{"raw": string(tc.Arguments)}
		}
		id := tc.ID
		if id == "" {
			id = fmt.Sprintf("call_%d", i)
		}
		calls = append(calls, ToolCall{
			ID:        id,
			Name:      tc.Name,
			Arguments: args,
		})
	}

	return calls
}

type StreamEventType string

const (
	EventReasoningStart StreamEventType = "reasoning_start"
	EventReasoningDelta StreamEventType = "reasoning_delta"
	EventReasoningEnd   StreamEventType = "reasoning_end"
	EventTextStart      StreamEventType = "text_start"
	EventTextDelta      StreamEventType = "text_delta"
	EventTextEnd        StreamEventType = "text_end"
	EventToolInputStart StreamEventType = "tool_input_start"
	EventToolInputDelta StreamEventType = "tool_input_delta"
	EventToolInputEnd   StreamEventType = "tool_input_end"
	EventToolCall       StreamEventType = "tool_call"
	EventToolResult     StreamEventType = "tool_result"
	EventToolError      StreamEventType = "tool_error"
	EventAssistantDelta StreamEventType = "assistant_delta"
	EventFinish         StreamEventType = "finish"
)

type StreamEvent struct {
	Type         StreamEventType
	Index        int
	Text         string
	ToolName     string
	ToolCallID   string
	ToolArgs     string
	ReasoningID  string
	FinishReason string
	Error        error
	Input        map[string]any
	Output       any
	ProviderMeta any
}

type StreamHandler interface {
	HandleEvent(ctx context.Context, event StreamEvent) error
}

type ToolExecutor func(ctx context.Context, toolName string, input map[string]any) (map[string]any, error)

func NewToolExecutor(registry ToolRegistry, config *ToolConfig) ToolExecutor {
	if config == nil {
		config = DefaultToolConfig()
	}

	return func(ctx context.Context, toolName string, input map[string]any) (map[string]any, error) {
		if !config.IsToolAllowed(toolName) {
			return nil, fmt.Errorf("tool %s is not allowed", toolName)
		}

		originalName := toolName
		tool, ok := registry.Get(toolName)
		if !ok {
			normalizedName := config.NormalizeToolName(toolName)
			if normalizedName != toolName {
				tool, ok = registry.Get(normalizedName)
				if ok {
					toolName = normalizedName
				}
			}
		}

		if !ok {
			return nil, fmt.Errorf("tool %s not found (tried: %s)", originalName, toolName)
		}

		result, err := tool.Invoke(ctx, input)
		if err != nil {
			return nil, err
		}

		if !result.Success {
			return result.Data, fmt.Errorf("%s", result.Error)
		}

		return result.Data, nil
	}
}

type EvaluationResult int

const (
	EvalResultUnknown EvaluationResult = iota
	EvalResultSuccess
	EvalResultIncomplete
	EvalResultFailed
)

func (e EvaluationResult) String() string {
	switch e {
	case EvalResultSuccess:
		return "success"
	case EvalResultIncomplete:
		return "incomplete"
	case EvalResultFailed:
		return "failed"
	default:
		return "unknown"
	}
}

type ToolEvaluator struct {
	successIndicators    map[string][]string
	failureIndicators    map[string][]string
	incompleteIndicators map[string][]string
}

func NewToolEvaluator() *ToolEvaluator {
	return &ToolEvaluator{
		successIndicators:    make(map[string][]string),
		failureIndicators:    make(map[string][]string),
		incompleteIndicators: make(map[string][]string),
	}
}

func (te *ToolEvaluator) RegisterSuccessIndicator(toolName, pattern string) {
	te.successIndicators[toolName] = append(te.successIndicators[toolName], pattern)
}

func (te *ToolEvaluator) RegisterFailureIndicator(toolName, pattern string) {
	te.failureIndicators[toolName] = append(te.failureIndicators[toolName], pattern)
}

func (te *ToolEvaluator) RegisterIncompleteIndicator(toolName, pattern string) {
	te.incompleteIndicators[toolName] = append(te.incompleteIndicators[toolName], pattern)
}

func (te *ToolEvaluator) Evaluate(toolName string, result ToolResult) (EvaluationResult, string) {
	if !result.Success {
		return EvalResultFailed, result.Error
	}

	dataStr := fmt.Sprintf("%v", result.Data)

	if result.Error != "" {
		return EvalResultFailed, result.Error
	}

	for _, pattern := range te.failureIndicators[toolName] {
		if strings.Contains(dataStr, pattern) || strings.Contains(result.Error, pattern) {
			return EvalResultFailed, fmt.Sprintf("failure indicator matched: %s", pattern)
		}
	}

	for _, pattern := range te.incompleteIndicators[toolName] {
		if strings.Contains(dataStr, pattern) || strings.Contains(result.Error, pattern) {
			return EvalResultIncomplete, fmt.Sprintf("incomplete indicator matched: %s", pattern)
		}
	}

	for _, pattern := range te.successIndicators[toolName] {
		if strings.Contains(dataStr, pattern) && !strings.Contains(result.Error, pattern) {
			return EvalResultSuccess, fmt.Sprintf("success indicator matched: %s", pattern)
		}
	}

	if dataStr == "" || dataStr == "map[]" || dataStr == "{}" {
		return EvalResultIncomplete, "empty result"
	}

	if strings.Contains(dataStr, "error") || strings.Contains(dataStr, "failed") {
		lower := strings.ToLower(dataStr)
		if strings.Contains(lower, "error") && !strings.Contains(lower, "no error") && !strings.Contains(lower, "without error") {
			return EvalResultIncomplete, "result contains error indicators"
		}
	}

	return EvalResultSuccess, "no specific indicators matched"
}

func (te *ToolEvaluator) BuildRecoveryPrompt(toolName string, evalResult EvaluationResult, reason string, attemptNum int) string {
	base := fmt.Sprintf("Tool '%s' evaluation: %s (%s).", toolName, evalResult.String(), reason)

	if evalResult == EvalResultFailed {
		return fmt.Sprintf("%s The previous attempt failed. Try a different approach or alternative tool to accomplish the task. This is attempt #%d.", base, attemptNum)
	}

	if evalResult == EvalResultIncomplete {
		return fmt.Sprintf("%s The previous result was incomplete. Try to complete the task using a different method or tool. This is attempt #%d.", base, attemptNum)
	}

	return fmt.Sprintf("%s Review the result and continue if the task is complete, or try another approach if not. This is attempt #%d.", base, attemptNum)
}

func DefaultToolEvaluator() *ToolEvaluator {
	te := NewToolEvaluator()

	te.RegisterFailureIndicator("web_fetch", "error")
	te.RegisterFailureIndicator("web_fetch", "failed to fetch")
	te.RegisterFailureIndicator("web_fetch", "connection refused")
	te.RegisterFailureIndicator("web_fetch", "timeout")
	te.RegisterFailureIndicator("web_fetch", "404")
	te.RegisterFailureIndicator("web_fetch", "500")
	te.RegisterFailureIndicator("web_fetch", "502")
	te.RegisterFailureIndicator("web_fetch", "503")

	te.RegisterIncompleteIndicator("web_fetch", "no results")
	te.RegisterIncompleteIndicator("web_fetch", "empty result")
	te.RegisterIncompleteIndicator("web_fetch", "not found")

	te.RegisterSuccessIndicator("web_fetch", "200")
	te.RegisterSuccessIndicator("web_fetch", "success")
	te.RegisterSuccessIndicator("web_fetch", "<!DOCTYPE")
	te.RegisterSuccessIndicator("web_fetch", "<html")

	te.RegisterFailureIndicator("fs_read", "no such file")
	te.RegisterFailureIndicator("fs_read", "permission denied")
	te.RegisterFailureIndicator("fs_read", "is a directory")

	te.RegisterIncompleteIndicator("fs_read", "empty")

	te.RegisterSuccessIndicator("fs_read", "read")
	te.RegisterSuccessIndicator("fs_read", "content")

	te.RegisterFailureIndicator("fs_write", "error")
	te.RegisterFailureIndicator("fs_write", "failed to write")
	te.RegisterFailureIndicator("fs_write", "permission denied")

	te.RegisterSuccessIndicator("fs_write", "written")
	te.RegisterSuccessIndicator("fs_write", "saved")

	te.RegisterFailureIndicator("cmd_exec", "error")
	te.RegisterFailureIndicator("cmd_exec", "command not found")
	te.RegisterFailureIndicator("cmd_exec", "permission denied")
	te.RegisterFailureIndicator("cmd_exec", "exited with code")

	te.RegisterSuccessIndicator("cmd_exec", "exited with code 0")

	return te
}

type StreamProcessor interface {
	Process(event StreamEvent) error
	Flush() error
	Close() error
}

type StreamProcessorFunc func(event StreamEvent) error

func (f StreamProcessorFunc) Process(event StreamEvent) error {
	return f(event)
}

func (f StreamProcessorFunc) Flush() error {
	return nil
}

func (f StreamProcessorFunc) Close() error {
	return nil
}

type BufferedStreamProcessor struct {
	bufferSize int
	buffer     []StreamEvent
	handler    StreamHandler
	flushFn    func([]StreamEvent) error
}

func NewBufferedStreamProcessor(bufferSize int, handler StreamHandler) *BufferedStreamProcessor {
	return &BufferedStreamProcessor{
		bufferSize: bufferSize,
		buffer:     make([]StreamEvent, 0, bufferSize),
		handler:    handler,
	}
}

func (p *BufferedStreamProcessor) Process(event StreamEvent) error {
	p.buffer = append(p.buffer, event)

	if len(p.buffer) >= p.bufferSize || event.Type == EventFinish {
		return p.Flush()
	}
	return nil
}

func (p *BufferedStreamProcessor) Flush() error {
	if len(p.buffer) == 0 {
		return nil
	}

	for _, event := range p.buffer {
		if err := p.handler.HandleEvent(context.Background(), event); err != nil {
			return err
		}
	}

	p.buffer = p.buffer[:0]
	return nil
}

func (p *BufferedStreamProcessor) Close() error {
	return p.Flush()
}

func (p *BufferedStreamProcessor) WithFlushFn(fn func([]StreamEvent) error) *BufferedStreamProcessor {
	p.flushFn = fn
	return p
}

type TransformStreamProcessor struct {
	handlers []StreamHandler
}

func NewTransformStreamProcessor() *TransformStreamProcessor {
	return &TransformStreamProcessor{
		handlers: make([]StreamHandler, 0),
	}
}

func (p *TransformStreamProcessor) AddHandler(h StreamHandler) *TransformStreamProcessor {
	p.handlers = append(p.handlers, h)
	return p
}

func (p *TransformStreamProcessor) Process(event StreamEvent) error {
	for _, h := range p.handlers {
		if err := h.HandleEvent(context.Background(), event); err != nil {
			return err
		}
	}
	return nil
}

func (p *TransformStreamProcessor) Flush() error {
	return nil
}

func (p *TransformStreamProcessor) Close() error {
	return nil
}

type FilterStreamProcessor struct {
	filter func(StreamEvent) bool
	next   StreamProcessor
}

func NewFilterStreamProcessor(filter func(StreamEvent) bool, next StreamProcessor) *FilterStreamProcessor {
	return &FilterStreamProcessor{
		filter: filter,
		next:   next,
	}
}

func (p *FilterStreamProcessor) Process(event StreamEvent) error {
	if p.filter(event) {
		return p.next.Process(event)
	}
	return nil
}

func (p *FilterStreamProcessor) Flush() error {
	return p.next.Flush()
}

func (p *FilterStreamProcessor) Close() error {
	return p.next.Close()
}

type MapStreamProcessor struct {
	transform func(StreamEvent) StreamEvent
	next      StreamProcessor
}

func NewMapStreamProcessor(transform func(StreamEvent) StreamEvent, next StreamProcessor) *MapStreamProcessor {
	return &MapStreamProcessor{
		transform: transform,
		next:      next,
	}
}

func (p *MapStreamProcessor) Process(event StreamEvent) error {
	transformed := p.transform(event)
	return p.next.Process(transformed)
}

func (p *MapStreamProcessor) Flush() error {
	return p.next.Flush()
}

func (p *MapStreamProcessor) Close() error {
	return p.next.Close()
}

type StreamAggregator struct {
	mu           sync.Mutex
	text         strings.Builder
	reasoning    strings.Builder
	toolCalls    []StreamEvent
	toolCallID   string
	toolCallName string
	currentTool  int
}

func NewStreamAggregator() *StreamAggregator {
	return &StreamAggregator{
		toolCalls: make([]StreamEvent, 0),
	}
}

func (a *StreamAggregator) Process(event StreamEvent) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	switch event.Type {
	case EventTextDelta:
		a.text.WriteString(event.Text)
	case EventReasoningDelta:
		a.reasoning.WriteString(event.Text)
	case EventToolInputStart:
		if a.currentTool < len(a.toolCalls) {
			a.toolCalls[a.currentTool] = event
		} else {
			a.toolCalls = append(a.toolCalls, event)
		}
	case EventToolCall:
		a.toolCalls = append(a.toolCalls, event)
		a.currentTool++
	}
	return nil
}

func (a *StreamAggregator) Text() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.text.String()
}

func (a *StreamAggregator) Reasoning() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.reasoning.String()
}

func (a *StreamAggregator) ToolCalls() []StreamEvent {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.toolCalls
}

func (a *StreamAggregator) Reset() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.text.Reset()
	a.reasoning.Reset()
	a.toolCalls = a.toolCalls[:0]
	a.currentTool = 0
}
