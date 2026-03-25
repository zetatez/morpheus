package webfetch

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type FetchTool struct {
	client *http.Client
}

func NewFetchTool() *FetchTool {
	return &FetchTool{
		client: &http.Client{
			Transport: &http.Transport{
				MaxIdleConns:    100,
				MaxConnsPerHost: 10,
				IdleConnTimeout: 90 * time.Second,
			},
			Timeout: 30 * time.Second,
		},
	}
}

func (t *FetchTool) Name() string { return "webfetch" }

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

// isPrivateIP checks if an IP address is private or reserved
func isPrivateIP(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	privateBlocks := []string{
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16", // link-local
		"127.0.0.0/8",    // loopback
		"::1/128",        // IPv6 loopback
		"fc00::/7",       // IPv6 unique local
		"fe80::/10",      // IPv6 link-local
	}
	for _, block := range privateBlocks {
		_, cidr, _ := net.ParseCIDR(block)
		if cidr.Contains(parsedIP) {
			return true
		}
	}
	return false
}

// isMetadataEndpoint checks if the host is a cloud metadata endpoint
func isMetadataEndpoint(host string) bool {
	metadataHosts := []string{
		"metadata.google.internal",
		"169.254.169.254",
		"metadata.azure.com",
		"instance-data",
	}
	for _, m := range metadataHosts {
		if strings.HasPrefix(host, m) {
			return true
		}
	}
	return false
}

func (t *FetchTool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	urlStr, _ := input["url"].(string)
	if urlStr == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("url is required")
	}

	// Validate URL to prevent SSRF
	if !isAllowedURL(urlStr) {
		return sdk.ToolResult{Success: false}, fmt.Errorf("url not allowed: must use http/https and cannot access private IPs or metadata endpoints")
	}

	method, _ := input["method"].(string)
	if method == "" {
		method = "GET"
	}

	body, _ := input["body"].(string)

	req, err := http.NewRequestWithContext(ctx, method, urlStr, bytes.NewReader([]byte(body)))
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

// isAllowedURL validates that a URL is safe to fetch
func isAllowedURL(rawURL string) bool {
	u, err := url.Parse(rawURL)
	if err != nil {
		return false
	}
	// Only allow http and https
	if u.Scheme != "http" && u.Scheme != "https" {
		return false
	}
	host := u.Hostname()
	// Block localhost and private IPs
	if host == "localhost" || host == "127.0.0.1" || isPrivateIP(host) || isMetadataEndpoint(host) {
		return false
	}
	return true
}
