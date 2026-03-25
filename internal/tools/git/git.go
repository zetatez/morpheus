package git

import (
	"bytes"
	"context"
	"os/exec"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type DiffTool struct {
	workspace string
}

type StatusTool struct {
	workspace string
}

func NewDiffTool(workspace string) *DiffTool {
	return &DiffTool{workspace: workspace}
}

func NewStatusTool(workspace string) *StatusTool {
	return &StatusTool{workspace: workspace}
}

func (t *DiffTool) Name() string { return "git.diff" }

func (t *DiffTool) Describe() string { return "Show git diff output." }

func (t *DiffTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"args": map[string]any{"type": "string"},
		},
	}
}

func (t *DiffTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	args, _ := input["args"].(string)
	if args == "" {
		args = "--stat"
	}
	return runGitCmd(ctx, t.workspace, "diff", args)
}

func (t *StatusTool) Name() string { return "git.status" }

func (t *StatusTool) Describe() string { return "Show git status output." }

func (t *StatusTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"flags": map[string]any{"type": "string"},
		},
	}
}

func (t *StatusTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	flags, _ := input["flags"].(string)
	if flags == "" {
		flags = "-s"
	}
	return runGitCmd(ctx, t.workspace, "status", flags)
}

func runGitCmd(ctx context.Context, dir, subcmd string, args string) (sdk.ToolResult, error) {
	cmd := exec.CommandContext(ctx, "git", subcmd)
	if args != "" {
		cmd.Args = append(cmd.Args, args)
	}
	cmd.Dir = dir
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return sdk.ToolResult{
		Success: err == nil,
		Data: map[string]any{
			"stdout": stdout.String(),
			"stderr": stderr.String(),
		},
		Error: errStr(err),
	}, nil
}

func errStr(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
