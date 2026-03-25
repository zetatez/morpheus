package cmd

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"

	"github.com/zetatez/morpheus/pkg/sdk"
)

// ExecTool executes shell commands inside the workspace root.
type ExecTool struct {
	workspace string
	timeout   time.Duration
}

// NewExecTool constructs a cmd.exec tool.
func NewExecTool(workspace string, timeout time.Duration) *ExecTool {
	if timeout == 0 {
		timeout = 2 * time.Minute
	}
	return &ExecTool{workspace: workspace, timeout: timeout}
}

func (t *ExecTool) Name() string { return "cmd.exec" }

func (t *ExecTool) Describe() string { return "Execute a shell command in the workspace." }

func (t *ExecTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{"type": "string"},
			"workdir": map[string]any{"type": "string"},
		},
		"required": []string{"command"},
	}
}

func (t *ExecTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	command, _ := input["command"].(string)
	if command == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("command is required")
	}
	workdir, _ := input["workdir"].(string)
	if workdir == "" {
		workdir = t.workspace
	}
	runCtx, cancel := context.WithTimeout(ctx, t.timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, "bash", "-c", command)
	cmd.Dir = workdir
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	result := sdk.ToolResult{
		Success: err == nil,
		Data: map[string]any{
			"stdout":    stdout.String(),
			"stderr":    stderr.String(),
			"exit_code": cmd.ProcessState.ExitCode(),
		},
	}
	if err != nil {
		result.Error = err.Error()
		return result, nil
	}
	return result, nil
}
