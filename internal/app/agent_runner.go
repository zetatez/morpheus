package app

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/zetatez/morpheus/internal/exec"
	"github.com/zetatez/morpheus/pkg/sdk"
)

const (
	reflectionEnabled        = true
	maxReflectionSuggestions = 3
)

type reflectionState struct {
	consecutiveFailures  int
	consecutiveSuccesses int
	lastReflection       string
	suggestions          []string
}

func newReflectionState() *reflectionState {
	return &reflectionState{
		consecutiveFailures:  0,
		consecutiveSuccesses: 0,
		suggestions:          []string{},
	}
}

func (rs *reflectionState) recordResult(success bool) {
	if success {
		rs.consecutiveSuccesses++
		rs.consecutiveFailures = 0
	} else {
		rs.consecutiveFailures++
		rs.consecutiveSuccesses = 0
	}
}

func (rs *reflectionState) shouldReflect() bool {
	return rs.consecutiveFailures >= 2 || rs.consecutiveSuccesses >= 3
}

func (rs *reflectionState) addSuggestion(suggestion string) {
	if len(rs.suggestions) >= maxReflectionSuggestions {
		rs.suggestions = rs.suggestions[1:]
	}
	rs.suggestions = append(rs.suggestions, suggestion)
}

type runnerCallbacks struct {
	emit             replEmitter
	callChat         func(context.Context, []map[string]any, []map[string]any, any, replEmitter) (chatResponse, error)
	onAssistantDelta func(string)
	onToolPending    func(string, toolCall)
	onToolResult     func(string, string, sdk.PlanStep, sdk.ToolResult)
	onRunEvent       func(string, map[string]any)
	streaming        bool
}

func (rt *Runtime) finalizeRun(run *RunState, emit replEmitter, eventType string, data map[string]any) {
	if eventType != "" {
		_ = rt.emitRunEvent(run, emit, eventType, data)
	}
	rt.runFinalEvent(run, emit)
}

func (rt *Runtime) runAgentLoop(ctx context.Context, sessionID string, input UserInput, format *OutputFormat, mode AgentMode, cb runnerCallbacks) (Response, error) {
	return rt.runAgentLoopWithRun(ctx, nil, sessionID, input, format, mode, cb)
}

func (rt *Runtime) runAgentLoopWithRun(ctx context.Context, existingRun *RunState, sessionID string, input UserInput, format *OutputFormat, mode AgentMode, cb runnerCallbacks) (Response, error) {
	if sessionID == "" {
		sessionID = "default"
	}
	run := existingRun
	if run == nil {
		run = rt.startRun(sessionID, input, format, mode)
	}
	rt.logger.Info("runAgentLoopWithRun start", zap.String("run_id", run.ID), zap.String("session", sessionID), zap.String("input", input.Text))
	ctx, cancel := context.WithDeadline(ctx, run.Deadline)
	ctx = withTeamSession(ctx, sessionID)
	run.Cancel = cancel
	defer cancel()
	text := input.Text
	pending, hasPendingConfirmation := rt.getPendingConfirmation(sessionID)
	if cb.onRunEvent != nil {
		cb.onRunEvent("run_started", map[string]any{"run_id": run.ID, "session_id": sessionID, "deadline": run.Deadline})
	}
	if hasPendingConfirmation {
		return rt.handlePendingConfirmation(ctx, sessionID, text, pending, run, cb)
	}
	if resp, handled, cmdErr := rt.handleTeamCommand(ctx, sessionID, text); handled {
		if cmdErr != nil {
			finishRun(run, RunStatusFailed, "", cmdErr)
			rt.finalizeRun(run, cb.emit, "run_failed", map[string]any{"error": cmdErr.Error()})
			return Response{}, cmdErr
		}
		finishRun(run, RunStatusCompleted, resp.Reply, nil)
		rt.finalizeRun(run, cb.emit, "run_completed", map[string]any{"reply": resp.Reply})
		return resp, nil
	}
	if resp, handled, cmdErr := rt.handleCheckpointCommand(ctx, sessionID, text); handled {
		if cmdErr != nil {
			finishRun(run, RunStatusFailed, "", cmdErr)
			rt.finalizeRun(run, cb.emit, "run_failed", map[string]any{"error": cmdErr.Error()})
			return Response{}, cmdErr
		}
		finishRun(run, RunStatusCompleted, resp.Reply, nil)
		rt.finalizeRun(run, cb.emit, "run_completed", map[string]any{"reply": resp.Reply})
		return resp, nil
	}
	if updated, skillName, ok := rt.preprocessSkillCommand(ctx, text); ok {
		text = updated
		rt.allowSkill(sessionID, skillName)
	}

	normalized, err := rt.normalizeUserInput(ctx, UserInput{Text: text, Attachments: input.Attachments})
	if err != nil {
		finishRun(run, RunStatusFailed, "", err)
		rt.finalizeRun(run, cb.emit, "run_failed", map[string]any{"error": err.Error()})
		return Response{}, err
	}
	rt.logger.Info("runAgentLoopWithRun normalized input", zap.String("run_id", run.ID), zap.String("text", normalized.Text), zap.Int("parts", len(normalized.Parts)))
	run.Prompt = normalized.Text
	if _, err := rt.appendMessage(ctx, sessionID, "user", normalized.Text, normalized.Parts); err != nil {
		finishRun(run, RunStatusFailed, "", err)
		rt.finalizeRun(run, cb.emit, "run_failed", map[string]any{"error": err.Error()})
		return Response{}, err
	}
	rt.allowMentionedSkills(sessionID, normalized.Text)
	rt.allowMentionedSubagents(sessionID, normalized.Text)
	rt.setIsCodeTask(sessionID, rt.isCodeTask(sessionID) || looksLikeCodeTask([]sdk.Message{{Role: "user", Content: normalized.Text}}))
	rt.checkAndCompress(ctx, sessionID)
	classification := rt.classifyIntent(ctx, sessionID, normalized, format, false)
	route := requestRoute(classification.Route)
	if rt.isCodeTask(sessionID) && route != routeToolAgent && route != routeFreshInfo {
		route = routeLightweight
	}
	if cb.onRunEvent != nil && shouldEmitThinking(route) {
		cb.onRunEvent("thinking_started", map[string]any{"message": ""})
	}

	mode = normalizeAgentMode(mode)
	ctx = exec.WithAgentMode(ctx, string(mode))
	if !rt.cfg.Permissions.AllowAsk {
		ctx = exec.BlockAskTool(ctx)
	}

	allowedTools := exec.GetAllowedTools(ctx)
	if routeAllowed := allowedToolsForClassification(route, classification.SuggestedTools); routeAllowed != nil {
		allowedTools = routeAllowed
	}
	tools, toolChoice, nameMap, structuredName := rt.collectToolSpecs(ctx, format, mode, allowedTools)
	if route != routeToolAgent && route != routeFreshInfo {
		tools = nil
		toolChoice = nil
		nameMap = map[string]string{}
		structuredName = ""
	}
	rt.logger.Info("runAgentLoopWithRun collected tools", zap.String("run_id", run.ID), zap.String("route", formatRouteName(route)), zap.Strings("tags", classification.Tags), zap.Strings("suggested_tools", classification.SuggestedTools), zap.String("confidence", classification.Confidence), zap.Int("tool_count", len(tools)), zap.Any("tool_choice", toolChoice), zap.Int("name_map", len(nameMap)), zap.String("structured_name", structuredName), zap.Strings("tools", sortedToolNames(tools)))
	forkIsolated := forkIsolationFromContext(ctx)
	baseMessages := rt.buildMessagesForRoute(ctx, sessionID, route, forkIsolated)
	if normalized.Text != "" || len(normalized.Parts) > 0 {
		baseMessages = append(baseMessages, map[string]any{"role": "user", "content": normalized.Text})
	}
	if mode == AgentModePlan {
		baseMessages = append(baseMessages, map[string]any{"role": "system", "content": "Plan mode is read-only. If the user asks to execute commands or write files, respond with a plan first and ask them to switch to Build mode to apply changes."})
	}
	if summary, ok := rt.maybeCoordinate(ctx, sessionID, normalized.Text); ok {
		baseMessages = append(baseMessages, map[string]any{"role": "system", "content": summary})
	}
	if initialTodos := rt.planTodosFromInput(ctx, sessionID, normalized.Text); len(initialTodos) > 0 {
		rt.updateRunTodos(run, initialTodos, cb.emit)
		baseMessages = append(baseMessages, map[string]any{"role": "system", "content": "For this task, use the todo.write tool to keep the todo list current whenever scope changes or steps complete."})
		baseMessages = append(baseMessages, map[string]any{"role": "system", "content": renderTodoSystemPrompt(initialTodos)})
	}
	run.Messages = cloneMessages(baseMessages)
	run.Status = RunStatusRunning
	rt.logger.Info("runAgentLoopWithRun entering loop", zap.String("run_id", run.ID), zap.Int("message_count", len(baseMessages)))
	plan := sdk.Plan{Summary: normalized.Text, Status: sdk.PlanStatusInProgress}
	results := []sdk.ToolResult{}
	actionRepeats := map[string]int{}
	reflection := newReflectionState()
	planReq := sdk.PlanRequest{ConversationID: sessionID, Prompt: normalized.Text, Intent: "agent"}
	retries := 0
	freshInfoRetried := false
	freshInfoFetches := 0
	freshInfoSuccessfulFetches := 0
	if format != nil && format.Type == "json_schema" {
		if format.RetryCount > 0 {
			retries = format.RetryCount
		} else {
			retries = 2
		}
	}

	if route != routeToolAgent && route != routeFreshInfo {
		resp, err := cb.callChat(ctx, baseMessages, nil, nil, cb.emit)
		if err != nil {
			plan.Status = sdk.PlanStatusBlocked
			finishRun(run, RunStatusFailed, "", err)
			run.Plan = plan
			run.Results = results
			rt.finalizeRun(run, cb.emit, "run_failed", map[string]any{"error": err.Error()})
			return Response{Plan: plan, Results: results, Reply: ""}, err
		}
		if strings.TrimSpace(resp.Content) != "" {
			_, _ = rt.appendMessage(ctx, sessionID, "assistant", resp.Content, nil)
		}
		plan.Status = sdk.PlanStatusDone
		_ = rt.audit.Record(planReq, plan, results)
		_ = rt.persistSession(ctx, sessionID)
		finishRun(run, RunStatusCompleted, resp.Content, nil)
		run.Plan = plan
		run.Results = results
		rt.finalizeRun(run, cb.emit, "run_completed", map[string]any{"reply": resp.Content, "route": formatRouteName(route), "tags": classification.Tags, "confidence": classification.Confidence})
		return Response{Plan: plan, Results: results, Reply: resp.Content}, nil
	}

	for step := 0; step < maxAgentSteps; step++ {
		rt.logger.Info("runAgentLoopWithRun loop iteration", zap.String("run_id", run.ID), zap.Int("step", step+1))
		if err := ctx.Err(); err != nil {
			plan.Status = sdk.PlanStatusBlocked
			finishRun(run, RunStatusCancelled, "", err)
			run.Plan = plan
			run.Results = results
			rt.finalizeRun(run, cb.emit, "run_cancelled", map[string]any{"error": err.Error()})
			return Response{Plan: plan, Results: results, Reply: ""}, err
		}
		if cb.onRunEvent != nil {
			cb.onRunEvent("model_turn_started", map[string]any{"step": step + 1})
		}
		rt.logger.Info("runAgentLoopWithRun before callChat", zap.String("run_id", run.ID), zap.Int("step", step+1), zap.Int("messages", len(baseMessages)), zap.Int("tools", len(tools)))
		resp, err := cb.callChat(ctx, baseMessages, tools, toolChoice, cb.emit)
		if cb.onRunEvent != nil {
			cb.onRunEvent("model_turn_finished", map[string]any{"step": step + 1, "tool_calls": len(resp.ToolCalls), "content_len": len(resp.Content)})
		}
		rt.logger.Info("runAgentLoopWithRun after callChat", zap.String("run_id", run.ID), zap.Int("step", step+1), zap.Bool("err", err != nil), zap.String("finish_reason", resp.FinishReason), zap.Int("tool_calls", len(resp.ToolCalls)), zap.Int("content_len", len(resp.Content)))
		if err != nil {
			plan.Status = sdk.PlanStatusBlocked
			finishRun(run, RunStatusFailed, "", err)
			run.Plan = plan
			run.Results = results
			rt.finalizeRun(run, cb.emit, "run_failed", map[string]any{"error": err.Error()})
			return Response{Plan: plan, Results: results, Reply: ""}, err
		}

		if len(run.Todos) == 0 && shouldCreateTodos(normalized.Text) && step == 0 {
			baseMessages = append(baseMessages, map[string]any{"role": "system", "content": "This is a complex task. Before any other meaningful work, you must call todo.write to create a concrete todo list. Do not proceed with file edits, shell commands, or other tool calls until todo.write has been used."})
			continue
		}
		if len(run.Todos) > 0 && len(resp.ToolCalls) > 0 && !containsTodoWriteCall(nameMap, resp.ToolCalls) && !hasCompletedTodo(run.Todos) && step == 0 {
			baseMessages = append(baseMessages, map[string]any{"role": "system", "content": "You started a complex task without updating todos. Call todo.write now to establish or confirm the working todo list, then continue."})
			continue
		}

		if format != nil && format.Type == "json_schema" {
			if output, ok := extractStructuredOutput(resp.ToolCalls, structuredName); ok {
				serialized, _ := json.Marshal(output)
				_, _ = rt.appendMessage(ctx, sessionID, "assistant", string(serialized), []sdk.MessagePart{outputPart(output)})
				plan.Status = sdk.PlanStatusDone
				_ = rt.audit.Record(planReq, plan, results)
				_ = rt.persistSession(ctx, sessionID)
				finishRun(run, RunStatusCompleted, string(serialized), nil)
				run.Plan = plan
				run.Results = results
				rt.finalizeRun(run, cb.emit, "run_completed", map[string]any{"reply": string(serialized)})
				return Response{Plan: plan, Results: results, Reply: string(serialized)}, nil
			}
			if resp.FinishReason != "tool_calls" && resp.FinishReason != "tool-calls" {
				if retries > 0 {
					retries--
					baseMessages = append(baseMessages, map[string]any{"role": "system", "content": "You must call the StructuredOutput tool with a JSON object matching the schema."})
					continue
				}
				plan.Status = sdk.PlanStatusBlocked
				err := fmt.Errorf("structured output not produced")
				finishRun(run, RunStatusFailed, "", err)
				run.Plan = plan
				run.Results = results
				rt.finalizeRun(run, cb.emit, "run_failed", map[string]any{"error": err.Error()})
				return Response{Plan: plan, Results: results, Reply: ""}, err
			}
		}

		if len(resp.ToolCalls) == 0 {
			advanceTodosFromResponse(rt, run, resp.Content, cb.emit)
			if route == routeFreshInfo && !freshInfoRetried {
				baseMessages = append(baseMessages, map[string]any{"role": "system", "content": "This is a fresh-information request. You must call web.fetch before answering. Try a public source now and then summarize briefly."})
				freshInfoRetried = true
				continue
			}
			if resp.Content != "" {
				_, _ = rt.appendMessage(ctx, sessionID, "assistant", resp.Content, nil)
			}
			plan.Status = sdk.PlanStatusDone
			_ = rt.audit.Record(planReq, plan, results)
			_ = rt.persistSession(ctx, sessionID)
			finishRun(run, RunStatusCompleted, resp.Content, nil)
			run.Plan = plan
			run.Results = results
			rt.finalizeRun(run, cb.emit, "run_completed", map[string]any{"reply": resp.Content})
			return Response{Plan: plan, Results: results, Reply: resp.Content}, nil
		}

		assistantMessage := map[string]any{"role": "assistant", "content": resp.Content, "tool_calls": buildToolCallPayload(resp.ToolCalls)}
		baseMessages = append(baseMessages, assistantMessage)
		run.Messages = cloneMessages(baseMessages)

		for _, call := range resp.ToolCalls {
			toolName := nameMap[call.Name]
			if toolName == "" {
				toolName = call.Name
			}
			fingerprint := actionFingerprint(toolName, call.Arguments)
			actionRepeats[fingerprint]++
			if actionRepeats[fingerprint] > 2 {
				loopErr := fmt.Errorf("detected repeated tool call for %s; stopping to avoid a loop", toolName)
				finishRun(run, RunStatusFailed, "Stopped to avoid repeating the same failing step.", loopErr)
				run.Plan = plan
				run.Results = results
				rt.finalizeRun(run, cb.emit, "run_loop_detected", map[string]any{"tool": toolName, "reply": run.Reply, "error": loopErr.Error()})
				return Response{Plan: plan, Results: results, Reply: run.Reply}, loopErr
			}
			run.LastStep = fmt.Sprintf("tool:%s", toolName)
			run.Status = RunStatusWaitingTool
			if cb.onToolPending != nil {
				cb.onToolPending(toolName, call)
			}
			if cb.onRunEvent != nil {
				cb.onRunEvent("tool_execution_started", map[string]any{"tool": toolName, "call_id": call.ID, "timeout_ms": run.ToolTimeout.Milliseconds()})
			}
			markTodoInProgress(rt, run, toolName, cb.emit)
			stepID := uuid.NewString()
			planStep := sdk.PlanStep{ID: stepID, Description: fmt.Sprintf("Tool call: %s", toolName), Tool: toolName, Inputs: call.Arguments, Status: sdk.StepStatusRunning}
			toolCtx, toolCancel := context.WithTimeout(ctx, run.ToolTimeout)
			result, execErr := rt.orchestrator.ExecuteStep(toolCtx, sessionID, planStep)
			toolCancel()
			rt.maybeCreateCheckpoint(ctx, sessionID, toolName, call.Arguments)
			result.StepID = stepID
			if execErr != nil {
				if confirmErr, ok := exec.IsConfirmationRequired(execErr); ok {
					pending := pendingConfirmation{Tool: confirmErr.Tool, Inputs: confirmErr.Inputs, Decision: confirmErr.Decision}
					rt.setPendingConfirmation(sessionID, pending)
					question := formatConfirmationPrompt(pending)
					_, _ = rt.appendMessage(ctx, sessionID, "assistant", question, nil)
					plan.Status = sdk.PlanStatusDone
					_ = rt.audit.Record(planReq, plan, results)
					_ = rt.persistSession(ctx, sessionID)
					payload := confirmationPayload(pending)
					finishRun(run, RunStatusWaitingUser, question, nil)
					run.Plan = plan
					run.Results = results
					run.Confirmation = payload
					rt.finalizeRun(run, cb.emit, "run_waiting_user", map[string]any{"reply": question, "confirmation": payload})
					return Response{Plan: plan, Results: results, Reply: question, Confirmation: payload}, nil
				}
				result.Success = false
				result.Error = execErr.Error()
			}
			result.Data = rt.truncateToolResult(ctx, sessionID, planStep.Tool, result.Data)
			if route == routeFreshInfo && toolName == "web.fetch" {
				freshInfoFetches++
				if result.Success {
					freshInfoSuccessfulFetches++
				}
			}
			results = append(results, result)
			run.Results = results
			if result.Success {
				planStep.Status = sdk.StepStatusSucceeded
			} else {
				planStep.Status = sdk.StepStatusFailed
			}
			advanceTodosFromTool(rt, run, toolName, result.Success, cb.emit)
			plan.Steps = append(plan.Steps, planStep)
			if cb.onToolResult != nil {
				cb.onToolResult(toolName, call.ID, planStep, result)
			}
			if cb.onRunEvent != nil {
				cb.onRunEvent("tool_execution_finished", map[string]any{"tool": toolName, "call_id": call.ID, "success": result.Success, "error": result.Error})
			}
			run.Status = RunStatusRunning

			if (toolName == "conversation.echo" || toolName == "conversation.ask") && result.Success {
				if text, ok := result.Data["text"].(string); ok && strings.TrimSpace(text) != "" {
					_, _ = rt.appendMessage(ctx, sessionID, "assistant", text, nil)
					plan.Status = sdk.PlanStatusDone
					rt.updateLastTaskNote(sessionID, &plan, results)
					_ = rt.audit.Record(planReq, plan, results)
					_ = rt.persistSession(ctx, sessionID)
					finishRun(run, RunStatusCompleted, text, nil)
					run.Plan = plan
					run.Results = results
					rt.finalizeRun(run, cb.emit, "run_completed", map[string]any{"reply": text})
					return Response{Plan: plan, Results: results, Reply: text}, nil
				}
				if toolName == "conversation.ask" {
					questionText := formatAskQuestion(result.Data)
					if strings.TrimSpace(questionText) != "" {
						_, _ = rt.appendMessage(ctx, sessionID, "assistant", questionText, nil)
						plan.Status = sdk.PlanStatusDone
						rt.updateLastTaskNote(sessionID, &plan, results)
						_ = rt.audit.Record(planReq, plan, results)
						_ = rt.persistSession(ctx, sessionID)
						finishRun(run, RunStatusCompleted, questionText, nil)
						run.Plan = plan
						run.Results = results
						rt.finalizeRun(run, cb.emit, "run_completed", map[string]any{"reply": questionText})
						return Response{Plan: plan, Results: results, Reply: questionText}, nil
					}
				}
			}

			partStatus := "completed"
			if !result.Success || result.Error != "" {
				partStatus = "error"
			}
			toolPart := sdk.MessagePart{Type: "tool", Tool: toolName, CallID: call.ID, Input: call.Arguments, Output: result.Data, Error: result.Error, Status: partStatus}
			_, _ = rt.appendMessage(ctx, sessionID, "assistant", fmt.Sprintf("Tool call: %s", toolName), []sdk.MessagePart{toolPart})

			baseMessages = append(baseMessages, map[string]any{"role": "tool", "name": call.Name, "tool_call_id": call.ID, "content": formatToolResultContent(result)})
			run.Messages = cloneMessages(baseMessages)

			rt.addEpisodicMemory(sessionID, fmt.Sprintf("Tool: %s, Success: %v, Summary: %s", toolName, result.Success, truncateLines(formatToolResultContent(result), 100)), []string{toolName})
			if rt.memorySystem(sessionID).ShouldExtract() {
				extracted := rt.memorySystem(sessionID).ExtractSemanticFromEpisodic(ctx)
				if extracted > 0 {
					rt.logger.Debug("extracted semantic memories", zap.Int("count", extracted))
				}
			}

			if !result.Success && result.Error != "" {
				recoveryPrompt := fmt.Sprintf("The previous tool call '%s' failed with error: %s. Please analyze the error and try an alternative approach.", toolName, result.Error)
				if route == routeFreshInfo && toolName == "web.fetch" && !freshInfoRetried {
					recoveryPrompt = fmt.Sprintf("The previous web.fetch call failed with error: %s. This is a fresh-information request. Try a different public source with web.fetch now, then answer briefly.", result.Error)
					freshInfoRetried = true
				}
				baseMessages = append(baseMessages, map[string]any{"role": "system", "content": recoveryPrompt})
			} else if route == routeFreshInfo && toolName == "web.fetch" && result.Success {
				switch {
				case freshInfoSuccessfulFetches >= 2:
					baseMessages = append(baseMessages, map[string]any{"role": "system", "content": "You now have enough fresh evidence from multiple successful web.fetch calls. Stop fetching and answer the user's question directly in one concise reply, citing the fetched source names or URLs briefly."})
				case freshInfoFetches >= 3:
					baseMessages = append(baseMessages, map[string]any{"role": "system", "content": "You already have a successful web.fetch result and have tried several sources. Stop browsing and answer directly using the best fetched evidence you have."})
				}
			}

			if reflectionEnabled {
				reflection.recordResult(result.Success)
				if reflection.shouldReflect() && step < maxAgentSteps-1 {
					memReflection := rt.memorySystem(sessionID).Reflect(ctx, results, normalized.Text)
					if !memReflection.Success {
						reflection.addSuggestion(memReflection.Feedback)
					}
					if memReflection.LoopDetected {
						reflection.addSuggestion("Loop detected: consider alternative strategy")
					}
					for _, sugg := range memReflection.Suggestions {
						reflection.addSuggestion(sugg)
					}
					reflectionPrompt := rt.buildReflectionPrompt(results, normalized.Text)
					if reflectionPrompt != "" {
						baseMessages = append(baseMessages, map[string]any{"role": "system", "content": reflectionPrompt})
						reflection.consecutiveSuccesses = 0
						reflection.consecutiveFailures = 0
					}
				}
			}

			rt.checkAndCompress(ctx, sessionID)
		}
	}

	plan.Status = sdk.PlanStatusBlocked
	err = fmt.Errorf("agent loop exceeded max steps")
	finishRun(run, RunStatusFailed, "", err)
	run.Plan = plan
	run.Results = results
	rt.finalizeRun(run, cb.emit, "run_failed", map[string]any{"error": err.Error()})
	return Response{Plan: plan, Results: results, Reply: ""}, err
}

func actionFingerprint(toolName string, args map[string]any) string {
	data, _ := json.Marshal(args)
	sum := sha256.Sum256(append([]byte(toolName+":"), data...))
	return fmt.Sprintf("%x", sum[:])
}

func (rt *Runtime) handlePendingConfirmation(ctx context.Context, sessionID string, text string, pending pendingConfirmation, run *RunState, cb runnerCallbacks) (Response, error) {
	if _, err := rt.appendMessage(ctx, sessionID, "user", text, nil); err != nil {
		return Response{}, err
	}
	if isConfirmationApproval(text) {
		rt.clearPendingConfirmation(sessionID)
		if pending.Kind == "checkpoint_rollback" {
			id, _ := pending.Inputs["id"].(string)
			drop, _ := pending.Inputs["drop"].(bool)
			output, err := rt.rollbackCheckpoint(sessionID, id, drop)
			if err != nil {
				finishRun(run, RunStatusFailed, "", err)
				rt.finalizeRun(run, cb.emit, "run_failed", map[string]any{"error": err.Error()})
				return Response{}, err
			}
			reply := fmt.Sprintf("Rolled back to checkpoint %s.", strings.TrimSpace(id))
			if drop {
				reply = fmt.Sprintf("Rolled back to checkpoint %s and dropped it.", strings.TrimSpace(id))
			}
			if strings.TrimSpace(output) != "" {
				reply += "\n" + output
			}
			_, _ = rt.appendMessage(ctx, sessionID, "assistant", reply, nil)
			_ = rt.persistSession(ctx, sessionID)
			finishRun(run, RunStatusCompleted, reply, nil)
			rt.finalizeRun(run, cb.emit, "run_completed", map[string]any{"reply": reply})
			return Response{Plan: sdk.Plan{Summary: "Checkpoint rollback", Status: sdk.PlanStatusDone}, Reply: reply}, nil
		}
		stepID := uuid.NewString()
		planStep := sdk.PlanStep{ID: stepID, Description: fmt.Sprintf("Tool call: %s", pending.Tool), Tool: pending.Tool, Inputs: pending.Inputs, Status: sdk.StepStatusRunning}
		result, execErr := rt.orchestrator.ExecuteStep(exec.WithConfirmation(ctx), sessionID, planStep)
		rt.maybeCreateCheckpoint(ctx, sessionID, pending.Tool, pending.Inputs)
		result.StepID = stepID
		if execErr != nil {
			result.Success = false
			result.Error = execErr.Error()
		}
		result.Data = rt.truncateToolResult(ctx, sessionID, planStep.Tool, result.Data)
		if result.Success {
			planStep.Status = sdk.StepStatusSucceeded
		} else {
			planStep.Status = sdk.StepStatusFailed
		}
		plan := sdk.Plan{Summary: "Approved action", Status: sdk.PlanStatusDone, Steps: []sdk.PlanStep{planStep}}
		reply := "Approved and executed."
		if !result.Success {
			reply = "Approval received, but the action failed: " + strings.TrimSpace(result.Error)
		}
		finishRun(run, RunStatusCompleted, reply, nil)
		run.Plan = plan
		run.Results = []sdk.ToolResult{result}
		rt.finalizeRun(run, cb.emit, "run_completed", map[string]any{"reply": reply})
		return Response{Plan: plan, Results: []sdk.ToolResult{result}, Reply: reply}, nil
	}
	if isConfirmationDenial(text) {
		rt.clearPendingConfirmation(sessionID)
		reply := "Cancelled the pending action."
		_, _ = rt.appendMessage(ctx, sessionID, "assistant", reply, nil)
		finishRun(run, RunStatusCancelled, reply, nil)
		rt.finalizeRun(run, cb.emit, "run_cancelled", map[string]any{"reply": reply})
		return Response{Plan: sdk.Plan{Summary: "Cancelled action", Status: sdk.PlanStatusDone}, Reply: reply}, nil
	}
	question := "Please reply 'approve' to proceed or 'deny' to cancel."
	_, _ = rt.appendMessage(ctx, sessionID, "assistant", question, nil)
	pendingPayload := confirmationPayload(pending)
	finishRun(run, RunStatusWaitingUser, question, nil)
	run.Confirmation = pendingPayload
	rt.finalizeRun(run, cb.emit, "run_waiting_user", map[string]any{"reply": question, "confirmation": pendingPayload})
	return Response{Plan: sdk.Plan{Summary: "Awaiting confirmation", Status: sdk.PlanStatusDone}, Reply: question, Confirmation: pendingPayload}, nil
}
