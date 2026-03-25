package webfetch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type FetchTool struct {
	client *http.Client
}

func NewFetchTool() *FetchTool {
	return &FetchTool{
		client: &http.Client{},
	}
}

func (t *FetchTool) Name() string { return "web.fetch" }

func (t *FetchTool) Describe() string { return "Fetch a URL over HTTP." }

func (t *FetchTool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"url":     map[string]any{"type": "string"},
			"method":  map[string]any{"type": "string"},
			"body":    map[string]any{"type": "string"},
			"headers": map[string]any{"type": "object"},
		},
		"required": []string{"url"},
	}
}

func (t *FetchTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	url, _ := input["url"].(string)
	if url == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("url is required")
	}

	method, _ := input["method"].(string)
	if method == "" {
		method = "GET"
	}

	body, _ := input["body"].(string)

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader([]byte(body)))
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}

	headers, ok := input["headers"].(map[string]any)
	if ok {
		for k, v := range headers {
			req.Header.Set(k, fmt.Sprintf("%v", v))
		}
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return sdk.ToolResult{Success: false}, err
	}

	contentType := resp.Header.Get("Content-Type")
	isText := strings.Contains(contentType, "text/") || strings.Contains(contentType, "application/json")

	result := sdk.ToolResult{
		Success: resp.StatusCode >= 200 && resp.StatusCode < 300,
		Data: map[string]any{
			"status":       resp.StatusCode,
			"headers":      resp.Header,
			"content_type": contentType,
		},
	}

	if isText {
		result.Data["body"] = string(respBody)
	} else {
		result.Data["body_size"] = len(respBody)
	}

	if !result.Success {
		result.Error = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	return result, nil
}
