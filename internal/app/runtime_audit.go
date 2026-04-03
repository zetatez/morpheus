package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type auditWriter struct {
	mu   sync.Mutex
	file *os.File
}

func newAuditWriter(path string) (*auditWriter, error) {
	if path == "" {
		return &auditWriter{}, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &auditWriter{file: file}, nil
}

func (w *auditWriter) Record(req sdk.PlanRequest, plan sdk.Plan, results []sdk.ToolResult) error {
	if w == nil || w.file == nil {
		return nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, step := range plan.Steps {
		var cmd string
		if c, ok := step.Inputs["command"].(string); ok {
			cmd = c
		} else if p, ok := step.Inputs["path"].(string); ok {
			cmd = step.Tool + " " + p
		}

		var output string
		for _, r := range results {
			if r.StepID == step.ID {
				if r.Success && r.Data != nil {
					if stdout, ok := r.Data["stdout"].(string); ok {
						output = stdout
					} else if content, ok := r.Data["content"].(string); ok {
						output = content
					}
				}
				if r.Error != "" {
					output = "Error: " + r.Error
				}
				break
			}
		}

		entry := map[string]any{
			"ts":      time.Now().Format("2006-01-02 15:04:05"),
			"tool":    step.Tool,
			"command": cmd,
			"output":  output,
		}
		data, err := json.Marshal(entry)
		if err != nil {
			return err
		}
		if _, err := w.file.Write(append(data, '\n')); err != nil {
			return err
		}
	}
	return nil
}

func (w *auditWriter) Close() error {
	if w == nil || w.file == nil {
		return nil
	}
	return w.file.Close()
}
