package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/zetatez/morpheus/internal/exec"
	"github.com/zetatez/morpheus/pkg/sdk"
)

const (
	MAX_STEPS_MESSAGE         = "MAX_STEPS: Maximum number of iterations reached. Stopping."
	RETRY_BACKOFF_BASE_MS     = 500
	MAX_RETRY_BACKOFF_MS      = 8000
	MAX_RETRY_ATTEMPTS        = 5
	DOOM_LOOP_WARNING_MESSAGE = "WARNING: You appear to be repeating the same tool call pattern. Consider trying a different approach or strategy."
)

type AgentConfig struct {
	MaxSteps                int
	MaxRunDurationMs        int64
	MaxConsecutiveFailures  int
	MaxStepsWithoutProgress int
	MaxSimilarResults       int
	DoomLoopThreshold       int
	ContinueOnDeny          bool
	Model                   string
}

func DefaultAgentConfig() AgentConfig {
	return AgentConfig{
		MaxSteps:                500,
		MaxRunDurationMs:        60 * 60 * 1000,
		MaxConsecutiveFailures:  10,
		MaxStepsWithoutProgress: 50,
		MaxSimilarResults:       5,
		DoomLoopThreshold:       3,
		ContinueOnDeny:          false,
	}
}

type ProgressTracker struct {
	cfg                  AgentConfig
	consecutiveFailures  int
	stepsWithoutProgress int
	recentResultHashes   []uint64
	similarStreak        int
}

func NewProgressTracker(cfg AgentConfig) *ProgressTracker {
	return &ProgressTracker{
		cfg:                cfg,
		recentResultHashes: make([]uint64, 0, cfg.MaxSimilarResults),
	}
}

func (p *ProgressTracker) effectiveMaxConsecutiveFailures() int {
	if p.cfg.MaxConsecutiveFailures <= 0 {
		return 10
	}
	return p.cfg.MaxConsecutiveFailures
}

func (p *ProgressTracker) effectiveMaxStepsWithoutProgress() int {
	if p.cfg.MaxStepsWithoutProgress <= 0 {
		return 50
	}
	return p.cfg.MaxStepsWithoutProgress
}

func (p *ProgressTracker) effectiveMaxRunDurationMs() int64 {
	if p.cfg.MaxRunDurationMs <= 0 {
		return 60 * 60 * 1000
	}
	return p.cfg.MaxRunDurationMs
}

func (p *ProgressTracker) recordFailure() {
	p.consecutiveFailures++
}

func (p *ProgressTracker) recordSuccess(isProgress bool) {
	p.consecutiveFailures = 0
	if isProgress {
		p.stepsWithoutProgress = 0
		p.recentResultHashes = p.recentResultHashes[:0]
		p.similarStreak = 0
	} else {
		p.stepsWithoutProgress++
	}
}

func (p *ProgressTracker) recordResultHash(hash uint64) {
	if len(p.recentResultHashes) >= p.cfg.MaxSimilarResults {
		p.recentResultHashes = p.recentResultHashes[1:]
	}
	p.recentResultHashes = append(p.recentResultHashes, hash)
}

func (p *ProgressTracker) isSimilarToRecent(hash uint64) bool {
	if len(p.recentResultHashes) < 2 {
		return false
	}
	similarCount := 0
	for _, h := range p.recentResultHashes {
		if v2ComputeSimilarity(hash, h) >= 0.8 {
			similarCount++
		}
	}
	return similarCount >= len(p.recentResultHashes)/2
}

func v2ComputeSimilarity(a, b uint64) float64 {
	x := a ^ b
	return 1.0 - float64(bitCount(x))/64.0
}

func bitCount(x uint64) int {
	count := 0
	for x != 0 {
		count++
		x &= x - 1
	}
	return count
}

func (p *ProgressTracker) shouldStop() (string, bool) {
	if p.consecutiveFailures >= p.effectiveMaxConsecutiveFailures() {
		return fmt.Sprintf("stopped after %d consecutive tool failures", p.consecutiveFailures), true
	}
	if p.stepsWithoutProgress >= p.effectiveMaxStepsWithoutProgress() {
		return fmt.Sprintf("stopped after %d steps without progress", p.stepsWithoutProgress), true
	}
	if p.similarStreak >= p.cfg.MaxSimilarResults {
		return fmt.Sprintf("stopped after %d similar results (possible loop)", p.similarStreak), true
	}
	return "", false
}

type ToolHandler interface {
	Execute(ctx context.Context, sessionID string, step sdk.PlanStep) (sdk.ToolResult, error)
	TruncateResult(ctx context.Context, sessionID, toolName string, data map[string]interface{}) map[string]interface{}
}

type LoopCallbacks struct {
	OnToolExecute   func(ctx context.Context, sessionID string, toolName string, args map[string]interface{}) (sdk.ToolResult, error)
	OnMessageAppend func(ctx context.Context, sessionID, role, content string, parts []sdk.MessagePart) error
	OnToolResult    func(toolName string, callID string, result sdk.ToolResult)
	OnRunEvent      func(eventType string, data map[string]interface{})
	OnEmit          func(event string, data interface{}) error
	OnCallChat      func(ctx context.Context, messages []map[string]interface{}, tools []map[string]interface{}, toolChoice interface{}, emit func(string, interface{}) error) (ChatResponse, error)
}

type runLoop struct {
	config           AgentConfig
	processor        *SessionProcessor
	compaction       *CompactionHandler
	doomLoop         *LoopDoomDetector
	progress         *ProgressTracker
	callbacks        LoopCallbacks
	toolHandler      ToolHandler
	logger           *zap.Logger
	startTime        time.Time
	existingDeadline time.Time
	doomLoopCount    int
	injectedHint     bool
	costTracker      *CostTracker
}

func NewRunLoop(cfg AgentConfig, callbacks LoopCallbacks, toolHandler ToolHandler) *runLoop {
	if cfg.MaxSteps <= 0 {
		cfg.MaxSteps = 500
	}
	if cfg.DoomLoopThreshold <= 0 {
		cfg.DoomLoopThreshold = DOOM_LOOP_THRESHOLD
	}

	rl := &runLoop{
		config:      cfg,
		processor:   NewSessionProcessor(cfg.MaxSteps),
		compaction:  NewCompactionHandler(""),
		doomLoop:    NewLoopDoomDetector(cfg.DoomLoopThreshold),
		progress:    NewProgressTracker(cfg),
		callbacks:   callbacks,
		toolHandler: toolHandler,
		logger:      zap.NewNop(),
		startTime:   time.Now(),
	}

	if cfg.Model != "" {
		rl.costTracker = NewCostTracker(cfg.Model)
	}

	return rl
}

func (rl *runLoop) SetLogger(logger *zap.Logger) {
	rl.logger = logger
}

func (rl *runLoop) GetCostSnapshot() map[string]any {
	if rl.costTracker == nil {
		return nil
	}
	return rl.costTracker.Snapshot()
}

func (rl *runLoop) Run(ctx context.Context, sessionID string, messages []map[string]interface{}, tools []map[string]interface{}, format *OutputFormat) (LoopResult, error) {
	return rl.RunWithDeadline(ctx, sessionID, messages, tools, format, time.Time{})
}

func (rl *runLoop) RunWithDeadline(ctx context.Context, sessionID string, messages []map[string]interface{}, tools []map[string]interface{}, format *OutputFormat, existingDeadline time.Time) (LoopResult, error) {
	step := 0
	results := []sdk.ToolResult{}
	maxSteps := int64(rl.config.MaxSteps)
	rl.startTime = time.Now()
	rl.existingDeadline = existingDeadline
	rl.doomLoopCount = 0
	rl.injectedHint = false

	rl.processor.SetCallbacks(ProcessorCallbacks{
		OnToolCall: func(index int, name, id string, args map[string]interface{}) {
			if rl.callbacks.OnEmit != nil {
				_ = rl.callbacks.OnEmit("tool_pending", map[string]interface{}{
					"tool": name, "input": args, "call_id": id,
				})
			}
		},
		OnToolResult: func(id string, result sdk.ToolResult) {
			if rl.callbacks.OnToolResult != nil {
				rl.callbacks.OnToolResult("", id, result)
			}
		},
		OnTextDelta: func(text string) {
			if rl.callbacks.OnEmit != nil {
				_ = rl.callbacks.OnEmit("text_delta", map[string]string{"text": text})
			}
		},
	})

	for {
		select {
		case <-ctx.Done():
			return LoopResult{Status: LoopStatusStop, Error: ctx.Err()}, ctx.Err()
		default:
		}

		step++
		rl.processor.Reset()
		rl.processor.IncrementStep()

		if rl.callbacks.OnRunEvent != nil {
			rl.callbacks.OnRunEvent("model_turn_started", map[string]interface{}{"step": step})
		}

		if step > 1 {
			elapsed := time.Since(rl.startTime)
			maxDuration := rl.progress.effectiveMaxRunDurationMs()
			if !existingDeadline.IsZero() {
				deadlineDur := existingDeadline.Sub(rl.startTime)
				if deadlineDur > 0 && deadlineDur < time.Duration(maxDuration)*time.Millisecond {
					maxDuration = int64(deadlineDur.Milliseconds())
				}
			}
			if elapsed.Milliseconds() > maxDuration {
				return LoopResult{
					Status: LoopStatusStop,
					Error:  fmt.Errorf("stopped after %d minutes (time limit)", int(elapsed.Minutes())),
				}, nil
			}
		}

		isLastStep := maxSteps > 0 && int64(step) >= maxSteps
		messagesForLLM := messages
		if isLastStep {
			messagesForLLM = append(messagesForLLM, map[string]interface{}{
				"role":    "assistant",
				"content": MAX_STEPS_MESSAGE,
			})
		}

		if rl.callbacks.OnRunEvent != nil {
			rl.callbacks.OnRunEvent("model_turn_started", map[string]interface{}{"step": step, "is_last_step": isLastStep})
		}

		if rl.callbacks.OnCallChat == nil {
			return LoopResult{Status: LoopStatusStop, Error: fmt.Errorf("OnCallChat not implemented")}, fmt.Errorf("OnCallChat not implemented")
		}

		rl.logger.Info("calling OnCallChat", zap.Int("step", step), zap.Int("msgCount", len(messagesForLLM)), zap.Int("toolCount", len(tools)))
		resp, err := rl.callbacks.OnCallChat(ctx, messagesForLLM, tools, nil, rl.callbacks.OnEmit)
		rl.logger.Info("OnCallChat returned", zap.Int("step", step), zap.Duration("elapsed", time.Since(rl.startTime)), zap.Error(err))
		if err != nil {
			return LoopResult{Status: LoopStatusStop, Error: err}, err
		}

		if rl.costTracker != nil && (resp.Usage.InputTokens > 0 || resp.Usage.OutputTokens > 0) {
			rl.costTracker.RecordStep(step, resp.Usage)
		}

		hasToolCalls := len(resp.ToolCalls) > 0
		content := resp.Content
		finishReason := resp.FinishReason

		finishedWithoutToolCalls := !hasToolCalls &&
			finishReason != "tool_calls" &&
			finishReason != "tool-calls" &&
			finishReason != "unknown" &&
			content != ""

		if finishedWithoutToolCalls {
			if rl.callbacks.OnRunEvent != nil {
				rl.callbacks.OnRunEvent("loop_exit", map[string]interface{}{
					"step":   step,
					"reason": "finished",
				})
			}

			plan := sdk.Plan{
				Summary: content,
				Status:  sdk.PlanStatusDone,
				Steps:   buildPlanSteps(results),
			}

			return LoopResult{
				Status:   LoopStatusStop,
				Response: &plan,
			}, nil
		}

		if reason, shouldStop := rl.progress.shouldStop(); shouldStop {
			return LoopResult{
				Status: LoopStatusStop,
				Error:  fmt.Errorf("%s", reason),
			}, nil
		}

		if !hasToolCalls {
			rl.compaction.MarkCompacted()
			continue
		}

		assistantMsg := map[string]interface{}{
			"role":    "assistant",
			"content": content,
		}
		if len(resp.ToolCalls) > 0 {
			tcPayload := make([]map[string]interface{}, len(resp.ToolCalls))
			for i, tc := range resp.ToolCalls {
				argsJSON, _ := json.Marshal(tc.Arguments)
				tcPayload[i] = map[string]interface{}{
					"id":   tc.ID,
					"type": "function",
					"function": map[string]interface{}{
						"name":      tc.Name,
						"arguments": string(argsJSON),
					},
				}
			}
			assistantMsg["tool_calls"] = tcPayload
		}
		messages = append(messages, assistantMsg)

		for _, tc := range resp.ToolCalls {
			stepID := uuid.NewString()
			planStep := sdk.PlanStep{
				ID:          stepID,
				Description: fmt.Sprintf("Tool call: %s", tc.Name),
				Tool:        tc.Name,
				Inputs:      tc.Arguments,
				Status:      sdk.StepStatusRunning,
			}

			isDoomLoop, _ := rl.doomLoop.Record(sessionID, tc.Name, tc.Arguments)
			if isDoomLoop {
				rl.doomLoopCount++
				rl.logger.Warn("doom loop detected", zap.String("tool", tc.Name), zap.Int("count", rl.doomLoopCount))
				if rl.doomLoopCount >= 10 {
					return LoopResult{
						Status: LoopStatusStop,
						Error:  fmt.Errorf("stopped due to repeated tool calls (%s x%d)", tc.Name, rl.doomLoopCount),
					}, nil
				}
				injectDoomLoopWarning(messages, tc.Name, tc.Arguments)
				rl.doomLoop.Reset(sessionID)
				continue
			}

			result, confErr := rl.executeWithRetry(ctx, sessionID, planStep)
			result.StepID = stepID
			rl.logger.Info("executeWithRetry returned", zap.String("tool", tc.Name), zap.Bool("success", result.Success), zap.Error(confErr))

			if confErr != nil {
				creq, _ := exec.IsConfirmationRequired(confErr)
				pending := PendingConfirmation{
					Tool:      creq.Tool,
					Inputs:    creq.Inputs,
					Decision:  creq.Decision,
					Kind:      "tool_confirmation",
					CreatedAt: time.Now(),
				}
				return LoopResult{
					Status:       LoopStatusNeedsConfirmation,
					Confirmation: &pending,
				}, nil
			}

			if rl.callbacks.OnEmit != nil {
				step := map[string]interface{}{
					"id":          planStep.ID,
					"description": planStep.Description,
					"tool":        planStep.Tool,
					"inputs":      planStep.Inputs,
					"status":      "completed",
				}
				_ = rl.callbacks.OnEmit("tool_result", map[string]interface{}{
					"call_id": tc.ID,
					"step":    step,
					"result":  result,
				})
			}

			results = append(results, result)

			if rl.callbacks.OnToolResult != nil {
				rl.callbacks.OnToolResult(tc.Name, tc.ID, result)
			}

			toolMsg := map[string]interface{}{
				"role":         "tool",
				"name":         tc.Name,
				"content":      v2FormatToolResultContent(result),
				"tool_call_id": tc.ID,
			}
			messages = append(messages, toolMsg)

			if !result.Success {
				rl.progress.recordFailure()
				injectErrorFeedback(messages, tc.Name, tc.Arguments, result.Error)
			} else {
				isProgress := isModificationTool(tc.Name)
				rl.progress.recordSuccess(isProgress)
				if tc.Name != "question" && tc.Name != "todowrite" {
					rl.progress.recordResultHash(hashResult(result.Data))
				}
			}

			if reason, shouldStop := rl.progress.shouldStop(); shouldStop {
				if rl.progress.consecutiveFailures >= rl.progress.effectiveMaxConsecutiveFailures() && !rl.injectedHint {
					hintMsg := rl.generateStallRecoveryHint(results)
					messages = append(messages, map[string]interface{}{
						"role":    "system",
						"content": hintMsg,
					})
					rl.injectedHint = true
					rl.progress.consecutiveFailures = rl.progress.consecutiveFailures / 2
					continue
				}
				if rl.progress.stepsWithoutProgress >= rl.progress.effectiveMaxStepsWithoutProgress() && !rl.injectedHint {
					hintMsg := "SYSTEM: You're not making progress. Try summarizing what you've done so far and propose a simpler approach to complete the task."
					messages = append(messages, map[string]interface{}{
						"role":    "system",
						"content": hintMsg,
					})
					rl.injectedHint = true
					rl.progress.stepsWithoutProgress = rl.progress.stepsWithoutProgress / 2
					continue
				}
				return LoopResult{
					Status: LoopStatusStop,
					Error:  fmt.Errorf("%s", reason),
				}, nil
			}
		}

		if isLastStep {
			if step >= int(maxSteps)-5 {
				rl.compaction.MarkCompacted()
				hintMsg := "SYSTEM: Approaching maximum steps. Focus on completing the current task with the remaining iterations."
				messages = append(messages, map[string]interface{}{
					"role":    "system",
					"content": hintMsg,
				})
				rl.injectedHint = true
				isLastStep = false
				continue
			}
			break
		}

		rl.compaction.MarkCompacted()
	}

	if step >= int(maxSteps) {
		return LoopResult{
			Status:   LoopStatusNeedsFinalSummary,
			Response: buildMaxStepsPlan(step, results),
		}, nil
	}

	return LoopResult{Status: LoopStatusContinue}, nil
}

func v2FormatToolResultContent(result sdk.ToolResult) string {
	if result.Error != "" {
		return fmt.Sprintf("Error: %s", result.Error)
	}
	if result.Data == nil {
		return ""
	}
	if content, ok := result.Data["content"].(string); ok {
		return content
	}
	dataStr, _ := json.Marshal(result.Data)
	return string(dataStr)
}

func (rl *runLoop) executeToolCall(ctx context.Context, sessionID string, call map[string]interface{}, planStep sdk.PlanStep) sdk.ToolResult {
	toolName, ok := call["name"].(string)
	if !ok {
		return sdk.ToolResult{Success: false, Error: "missing tool name"}
	}

	args, ok := call["arguments"].(map[string]interface{})
	if !ok {
		args = make(map[string]interface{})
	}

	isDoomLoop, _ := rl.doomLoop.Record(sessionID, toolName, args)
	if isDoomLoop {
		return sdk.ToolResult{
			Success: false,
			Error:   fmt.Sprintf("doom loop detected for tool %s", toolName),
		}
	}

	if rl.callbacks.OnToolExecute == nil {
		return sdk.ToolResult{Success: false, Error: "OnToolExecute not implemented"}
	}

	result, err := rl.toolHandler.Execute(ctx, sessionID, planStep)
	if err != nil {
		return sdk.ToolResult{Success: false, Error: err.Error()}
	}

	result.Data = rl.toolHandler.TruncateResult(ctx, sessionID, toolName, result.Data)

	isProgress := isModificationTool(toolName)
	rl.progress.recordSuccess(isProgress)
	if toolName != "question" && toolName != "todowrite" {
		rl.progress.recordResultHash(hashResult(result.Data))
		if rl.progress.isSimilarToRecent(hashResult(result.Data)) {
			rl.progress.similarStreak++
		} else {
			rl.progress.similarStreak = 0
		}
	}

	return result
}

func (rl *runLoop) checkDoomLoop(sessionID, toolName string, args map[string]interface{}) (bool, bool) {
	return rl.doomLoop.Record(sessionID, toolName, args)
}

func getLastUserMessage(messages []map[string]interface{}) string {
	for i := len(messages) - 1; i >= 0; i-- {
		if role, ok := messages[i]["role"].(string); ok && role == "user" {
			if content, ok := messages[i]["content"].(string); ok {
				return content
			}
		}
	}
	return ""
}

func buildPlanSteps(results []sdk.ToolResult) []sdk.PlanStep {
	steps := make([]sdk.PlanStep, 0, len(results))
	for _, r := range results {
		step := sdk.PlanStep{
			ID:          uuid.NewString(),
			Description: "tool execution",
			Status:      sdk.StepStatusRunning,
		}
		if r.Success {
			step.Status = sdk.StepStatusSucceeded
		} else {
			step.Status = sdk.StepStatusFailed
		}
		steps = append(steps, step)
	}
	return steps
}

func isModificationTool(toolName string) bool {
	switch toolName {
	case "write", "edit", "cmd.exec":
		return true
	}
	return false
}

func hashResult(data map[string]interface{}) uint64 {
	if data == nil {
		return 0
	}
	dataBytes, _ := json.Marshal(data)
	hash := uint64(0)
	for _, b := range dataBytes {
		hash = hash*31 + uint64(b)
	}
	return hash
}

func (rl *runLoop) executeWithRetry(ctx context.Context, sessionID string, planStep sdk.PlanStep) (sdk.ToolResult, error) {
	var lastResult sdk.ToolResult
	backoffMs := RETRY_BACKOFF_BASE_MS

	for attempt := 0; attempt <= MAX_RETRY_ATTEMPTS; attempt++ {
		result, err := rl.toolHandler.Execute(ctx, sessionID, planStep)
		if err != nil {
			if creq, ok := exec.IsConfirmationRequired(err); ok {
				return sdk.ToolResult{Success: false, Error: err.Error()}, creq
			}
			lastResult = sdk.ToolResult{Success: false, Error: err.Error()}
		} else {
			lastResult = result
		}

		if lastResult.Success {
			return lastResult, nil
		}

		if attempt < MAX_RETRY_ATTEMPTS && isRetryableError(lastResult.Error) {
			rl.logger.Info("retrying tool execution", zap.String("tool", planStep.Tool), zap.Int("attempt", attempt+1), zap.String("backoff_ms", fmt.Sprintf("%d", backoffMs)))
			time.Sleep(time.Duration(backoffMs) * time.Millisecond)
			backoffMs *= 2
			if backoffMs > MAX_RETRY_BACKOFF_MS {
				backoffMs = MAX_RETRY_BACKOFF_MS
			}
			continue
		}
		return lastResult, nil
	}
	return lastResult, nil
}

func injectDoomLoopWarning(messages []map[string]interface{}, toolName string, args map[string]interface{}) {
	argStr := ""
	if argsBytes, err := json.Marshal(args); err == nil {
		argStr = string(argsBytes)
	}
	warningContent := fmt.Sprintf("%s\n\nTool '%s' with arguments %s has been called multiple times with the same inputs, suggesting a potential infinite loop. The system has blocked this call to prevent excessive resource usage. Consider:\n1. Analyzing why this call keeps failing\n2. Trying a different tool or approach\n3. Breaking down the task into smaller steps\n4. Checking if there's a circular dependency in your approach", DOOM_LOOP_WARNING_MESSAGE, toolName, argStr)

	messages = append(messages, map[string]interface{}{
		"role":    "system",
		"content": warningContent,
	})
}

func injectDoomLoopRecovery(messages []map[string]interface{}, toolName string, args map[string]interface{}, state *DoomLoopState) string {
	argStr := ""
	if argsBytes, err := json.Marshal(args); err == nil {
		argStr = string(argsBytes)
	}

	attempt := 1
	if state != nil {
		attempt = state.RecoveryCount + 1
	}

	var hintStr string
	if attempt >= 2 {
		hintStr = fmt.Sprintf("RECOVERY ATTEMPT %d: You've detected a potential infinite loop with tool '%s' (arguments: %s). The previous recovery strategy didn't work. Try a COMPLETELY DIFFERENT approach:\n1. Summarize what you've accomplished so far\n2. Identify the core problem differently\n3. Use a different tool or command\n4. Break down the task into smaller, simpler steps", attempt, toolName, argStr)
	} else {
		hintStr = fmt.Sprintf("RECOVERY ATTEMPT %d: Tool '%s' with arguments %s has been called repeatedly. Before continuing:\n1. Verify the file/command exists and is accessible\n2. Use `glob` to find correct paths\n3. Try a simpler command to verify functionality\n4. Consider summarizing progress and asking for guidance", attempt, toolName, argStr)
	}

	messages = append(messages, map[string]interface{}{
		"role":    "system",
		"content": hintStr,
	})
	return hintStr
}

func injectDoomLoopError(messages []map[string]interface{}, toolName string, args map[string]interface{}) {
	argStr := ""
	if argsBytes, err := json.Marshal(args); err == nil {
		argStr = string(argsBytes)
	}
	errorContent := fmt.Sprintf("DOOM LOOP FATAL: Tool '%s' with arguments %s has been called repeatedly after multiple recovery attempts. The system is now stopping execution to prevent resource exhaustion.\n\nPlease:\n1. Manually review what this tool call is trying to accomplish\n2. Check for typos, wrong paths, or circular dependencies\n3. Try a different approach manually", toolName, argStr)

	messages = append(messages, map[string]interface{}{
		"role":    "system",
		"content": errorContent,
	})
}

func injectErrorFeedback(messages []map[string]interface{}, toolName string, args map[string]interface{}, errorMsg string) {
	feedback := generateErrorFeedback(toolName, args, errorMsg)
	messages = append(messages, map[string]interface{}{
		"role":    "system",
		"content": feedback,
	})
}

func generateErrorFeedback(toolName string, args map[string]interface{}, errorMsg string) string {
	if errorMsg == "" {
		return fmt.Sprintf("Tool '%s' failed without an error message. Consider verifying the tool's requirements and parameters.", toolName)
	}

	var suggestions []string

	switch toolName {
	case "read":
		suggestions = []string{
			"Verify the file path exists and is accessible",
			"Check if you have read permissions for this file",
			"Try using an absolute path instead of a relative path",
		}
	case "write", "edit":
		suggestions = []string{
			"Verify the directory exists and is writable",
			"Check if you have write permissions",
			"Ensure the file path is correct",
		}
	case "cmd.exec":
		suggestions = []string{
			"Verify the command exists and is in your PATH",
			"Check if the command syntax is correct",
			"Try specifying the full path to the executable",
		}
	default:
		suggestions = []string{
			"Review the tool's required parameters",
			"Check if the tool is available and properly configured",
			"Try simplifying the inputs or breaking the task into smaller steps",
		}
	}

	suggestionStr := strings.Join(suggestions, "\n")
	return fmt.Sprintf("Tool '%s' failed with error: %s\n\nConsider the following troubleshooting steps:\n%s", toolName, errorMsg, suggestionStr)
}

func (rl *runLoop) generateStallRecoveryHint(results []sdk.ToolResult) string {
	if len(results) == 0 {
		return "SYSTEM: The task appears to be stuck. Try a completely different approach or break the problem into smaller steps."
	}

	failedCount := 0
	successCount := 0
	for _, r := range results {
		if r.Success {
			successCount++
		} else {
			failedCount++
		}
	}

	var summary strings.Builder
	summary.WriteString("SYSTEM: Progress Analysis:\n")
	summary.WriteString(fmt.Sprintf("- Completed steps: %d\n", successCount))
	summary.WriteString(fmt.Sprintf("- Failed steps: %d\n\n", failedCount))

	if failedCount > successCount {
		summary.WriteString("You've encountered more failures than successes. Consider:\n")
		summary.WriteString("1. Summarizing what has worked so far\n")
		summary.WriteString("2. Trying a simpler approach to complete the core task\n")
		summary.WriteString("3. Breaking down the remaining work into smaller, more manageable steps\n")
	} else {
		summary.WriteString("You've made progress but are now stuck. Consider:\n")
		summary.WriteString("1. Summarizing the current state and next steps\n")
		summary.WriteString("2. Focusing on completing the most critical part of the task\n")
		summary.WriteString("3. Asking for clarification if the goal is unclear\n")
	}

	return summary.String()
}

func generateMaxStepsSummary(step int, results []sdk.ToolResult) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("Maximum iterations (%d) reached.\n\n", step))

	successCount := 0
	failedCount := 0
	for _, r := range results {
		if r.Success {
			successCount++
		} else {
			failedCount++
		}
	}

	b.WriteString(fmt.Sprintf("Summary:\n"))
	b.WriteString(fmt.Sprintf("- Total iterations: %d\n", step))
	b.WriteString(fmt.Sprintf("- Successful steps: %d\n", successCount))
	b.WriteString(fmt.Sprintf("- Failed steps: %d\n", failedCount))

	if successCount > 0 {
		b.WriteString("\nContext has been compressed for future continuation.")
	}

	return b.String()
}

func buildMaxStepsPlan(step int, results []sdk.ToolResult) *sdk.Plan {
	successCount := 0
	failedCount := 0
	for _, r := range results {
		if r.Success {
			successCount++
		} else {
			failedCount++
		}
	}
	summary := fmt.Sprintf("Maximum iterations (%d) reached. Completed %d steps successfully, %d failed.",
		step, successCount, failedCount)
	return &sdk.Plan{
		Summary: summary,
		Status:  sdk.PlanStatusInProgress,
		Steps:   buildPlanSteps(results),
	}
}
