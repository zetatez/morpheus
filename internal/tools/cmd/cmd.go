package cmd

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/zetatez/morpheus/pkg/sdk"
)

const (
	defaultExecTimeout = 2 * time.Minute
	maxExecOutputBytes = 64 * 1024
	minExecTimeoutSec  = 1
	maxExecTimeoutSec  = 600
)

// ExecTool executes shell commands inside the workspace root.
type ExecTool struct {
	workspace string
	timeout   time.Duration
}

// NewExecTool constructs a cmd.exec tool.
func NewExecTool(workspace string, timeout time.Duration) *ExecTool {
	if timeout == 0 {
		timeout = defaultExecTimeout
	}
	return &ExecTool{workspace: workspace, timeout: timeout}
}

func (t *ExecTool) Name() string { return "cmd.exec" }

func (t *ExecTool) Describe() string {
	return "Execute a shell command with optional workdir and timeout, returning stdout, stderr, exit code, timeout status, and truncated-output metadata."
}

func (t *ExecTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command": map[string]any{
				"type":        "string",
				"description": "Shell command to execute.",
			},
			"workdir": map[string]any{
				"type":        "string",
				"description": "Optional working directory. Defaults to workspace root.",
			},
			"timeout_seconds": map[string]any{
				"type":        "integer",
				"description": "Optional timeout in seconds (1-600). Defaults to the tool timeout.",
				"minimum":     minExecTimeoutSec,
				"maximum":     maxExecTimeoutSec,
			},
		},
		"required": []string{"command"},
	}
}

func (t *ExecTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	command, _ := input["command"].(string)
	command = strings.TrimSpace(command)
	if command == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("command is required")
	}

	workdir, err := t.resolveWorkdir(input)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}

	timeout := t.resolveTimeout(input)
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "bash", "-c", command)
	cmd.Dir = workdir

	var stdoutBuf bytes.Buffer
	var stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	startedAt := time.Now()
	err = cmd.Run()
	duration := time.Since(startedAt)

	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
	exitCode := -1
	if cmd.ProcessState != nil {
		exitCode = cmd.ProcessState.ExitCode()
	}

	stdout, stdoutTruncated, stdoutBytes := truncateOutput(stdoutBuf.String(), maxExecOutputBytes)
	stderr, stderrTruncated, stderrBytes := truncateOutput(stderrBuf.String(), maxExecOutputBytes)

	data := map[string]any{
		"stdout":             stdout,
		"stderr":             stderr,
		"exit_code":          exitCode,
		"command":            command,
		"workdir":            workdir,
		"duration_ms":        duration.Milliseconds(),
		"timed_out":          timedOut,
		"timeout_seconds":    int(timeout / time.Second),
		"stdout_truncated":   stdoutTruncated,
		"stderr_truncated":   stderrTruncated,
		"stdout_bytes":       stdoutBytes,
		"stderr_bytes":       stderrBytes,
		"output_limit_bytes": maxExecOutputBytes,
	}

	result := sdk.ToolResult{Success: err == nil, Data: data}
	if err != nil {
		if timedOut {
			result.Error = fmt.Sprintf("command timed out after %ds", int(timeout/time.Second))
		} else {
			result.Error = err.Error()
		}
		return result, nil
	}
	return result, nil
}

func (t *ExecTool) resolveWorkdir(input map[string]any) (string, error) {
	workdir, _ := input["workdir"].(string)
	workdir = strings.TrimSpace(workdir)
	if workdir == "" {
		workdir = t.workspace
	}
	if !filepath.IsAbs(workdir) {
		workdir = filepath.Join(t.workspace, workdir)
	}
	resolved, err := filepath.Abs(workdir)
	if err != nil {
		return "", fmt.Errorf("resolve workdir: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("workdir not accessible: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("workdir is not a directory: %s", resolved)
	}
	return resolved, nil
}

func (t *ExecTool) resolveTimeout(input map[string]any) time.Duration {
	timeout := t.timeout
	value, ok := input["timeout_seconds"]
	if !ok {
		return timeout
	}
	seconds := intFromAny(value)
	if seconds < minExecTimeoutSec {
		seconds = minExecTimeoutSec
	}
	if seconds > maxExecTimeoutSec {
		seconds = maxExecTimeoutSec
	}
	return time.Duration(seconds) * time.Second
}

func intFromAny(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float32:
		return int(n)
	case float64:
		return int(n)
	default:
		return 0
	}
}

func truncateOutput(text string, limit int) (string, bool, int) {
	if limit <= 0 {
		return text, false, len(text)
	}
	byteLen := len(text)
	if byteLen <= limit {
		return text, false, byteLen
	}
	trimmed := text[:limit]
	if idx := strings.LastIndex(trimmed, "\n"); idx >= limit/2 {
		trimmed = trimmed[:idx]
	}
	trimmed = strings.TrimRight(trimmed, "\n")
	suffix := fmt.Sprintf("\n... [truncated %d bytes]", byteLen-len(trimmed))
	return trimmed + suffix, true, byteLen
}
