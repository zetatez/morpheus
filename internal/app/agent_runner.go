package app

func (rt *Runtime) finalizeRun(run *RunState, emit replEmitter, eventType string, data map[string]any) {
	if eventType != "" {
		_ = rt.emitRunEvent(run, emit, eventType, data)
	}
	rt.runFinalEvent(run, emit)

	if run.SessionID != "" {
		rt.updateShortTermMemoryLightweight(run.SessionID)
		rt.recordEpisodicEvent(run)
	}
}

func (rt *Runtime) recordEpisodicEvent(run *RunState) {
	var content string
	var tags []string

	switch run.Status {
	case RunStatusCompleted:
		reply := run.Reply
		if len(reply) > 200 {
			reply = reply[:200] + "..."
		}
		if reply != "" {
			content = "Task completed: " + reply
		} else {
			content = "Task completed successfully"
		}
		tags = []string{"completed"}
	case RunStatusFailed:
		errStr := run.Err
		if len(errStr) > 150 {
			errStr = errStr[:150] + "..."
		}
		if errStr != "" {
			content = "Task failed: " + errStr
		} else {
			content = "Task failed"
		}
		tags = []string{"failed"}
	case RunStatusCancelled:
		content = "Task cancelled"
		tags = []string{"cancelled"}
	default:
		return
	}

	if content == "" {
		return
	}

	inputText := run.Input.Text
	if len(inputText) > 100 {
		inputText = inputText[:100] + "..."
	}
	if inputText != "" {
		content = content + " | Input: " + inputText
	}

	rt.addEpisodicMemory(run.SessionID, content, tags)
}
