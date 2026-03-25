package app

func (rt *Runtime) finalizeRun(run *RunState, emit replEmitter, eventType string, data map[string]any) {
	if eventType != "" {
		_ = rt.emitRunEvent(run, emit, eventType, data)
	}
	rt.runFinalEvent(run, emit)
}
