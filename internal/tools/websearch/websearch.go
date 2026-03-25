package websearch

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/zetatez/morpheus/pkg/sdk"
)

type SearchProvider string

const (
	ProviderDuckDuckGo SearchProvider = "duckduckgo"
	ProviderGoogle     SearchProvider = "google"
	ProviderBing       SearchProvider = "bing"
)

type Tool struct {
	client     *http.Client
	provider   SearchProvider
	apiKey     string
	maxResults int
}

type SearchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"`
	Source      string `json:"source,omitempty"`
}

func NewTool(provider SearchProvider, apiKey string) *Tool {
	return &Tool{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		provider:   provider,
		apiKey:     apiKey,
		maxResults: 10,
	}
}

func (t *Tool) Name() string { return "websearch" }

func (t *Tool) Describe() string { return "Search the web for information." }

func (t *Tool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Search query",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return (default: 10)",
			},
			"provider": map[string]any{
				"type":        "string",
				"description": "Search provider: duckduckgo, google, bing",
			},
		},
		"required": []string{"query"},
	}
}

func (t *Tool) Invoke(ctx context.Context, input map[string]any) (sdk.ToolResult, error) {
	query, _ := input["query"].(string)
	if query == "" {
		return sdk.ToolResult{Success: false}, fmt.Errorf("query is required")
	}

	maxResults := 10
	if mr, ok := input["max_results"].(float64); ok {
		maxResults = int(mr)
	}

	var results []SearchResult
	var err error

	switch t.provider {
	case ProviderGoogle:
		results, err = t.searchGoogle(ctx, query, maxResults)
	case ProviderBing:
		results, err = t.searchBing(ctx, query, maxResults)
	default:
		results, err = t.searchDuckDuckGo(ctx, query, maxResults)
	}

	if err != nil {
		return sdk.ToolResult{Success: false}, fmt.Errorf("search failed: %w", err)
	}

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	resultData := make([]map[string]any, len(results))
	for i, r := range results {
		resultData[i] = map[string]any{
			"title":       r.Title,
			"url":         r.URL,
			"description": r.Description,
		}
	}

	return sdk.ToolResult{
		Success: true,
		Data: map[string]any{
			"query":   query,
			"results": resultData,
			"count":   len(resultData),
		},
	}, nil
}

func (t *Tool) searchDuckDuckGo(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	apiURL := fmt.Sprintf("https://api.duckduckgo.com/?q=%s&format=json&no_html=1&skip_disambig=1", url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var ddgResponse struct {
		Results []struct {
			Text string `json:"Text"`
			URL  string `json:"URL"`
		} `json:"Results"`
		RelatedTopics []struct {
			Text string `json:"Text"`
			URL  string `json:"URL"`
		} `json:"RelatedTopics"`
		AbstractText string `json:"AbstractText"`
		AbstractURL  string `json:"AbstractURL"`
		Heading      string `json:"Heading"`
	}

	if err := json.Unmarshal(body, &ddgResponse); err != nil {
		return nil, err
	}

	var results []SearchResult

	if ddgResponse.AbstractText != "" {
		results = append(results, SearchResult{
			Title:       ddgResponse.Heading,
			URL:         ddgResponse.AbstractURL,
			Description: ddgResponse.AbstractText,
			Source:      "DuckDuckGo",
		})
	}

	for _, r := range ddgResponse.Results {
		if len(results) >= maxResults {
			break
		}
		results = append(results, SearchResult{
			Title:       r.Text,
			URL:         r.URL,
			Description: r.Text,
			Source:      "DuckDuckGo",
		})
	}

	for _, r := range ddgResponse.RelatedTopics {
		if len(results) >= maxResults {
			break
		}
		if r.URL == "" {
			continue
		}
		results = append(results, SearchResult{
			Title:       r.Text,
			URL:         r.URL,
			Description: r.Text,
			Source:      "DuckDuckGo",
		})
	}

	return results, nil
}

func (t *Tool) searchGoogle(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if t.apiKey == "" {
		return nil, fmt.Errorf("Google Search API requires an API key")
	}

	searchURL := fmt.Sprintf("https://www.googleapis.com/customsearch/v1?q=%s&key=%s&num=%d", url.QueryEscape(query), t.apiKey, maxResults)

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var googleResponse struct {
		Items []struct {
			Title   string `json:"title"`
			Link    string `json:"link"`
			Snippet string `json:"snippet"`
		} `json:"items"`
	}

	if err := json.Unmarshal(body, &googleResponse); err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(googleResponse.Items))
	for _, item := range googleResponse.Items {
		results = append(results, SearchResult{
			Title:       item.Title,
			URL:         item.Link,
			Description: item.Snippet,
			Source:      "Google",
		})
	}

	return results, nil
}

func (t *Tool) searchBing(ctx context.Context, query string, maxResults int) ([]SearchResult, error) {
	if t.apiKey == "" {
		return nil, fmt.Errorf("Bing Search API requires an API key")
	}

	endpoint := fmt.Sprintf("https://api.bing.microsoft.com/v7.0/search?q=%s", url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Ocp-Apim-Subscription-Key", t.apiKey)

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var bingResponse struct {
		WebPages struct {
			Value []struct {
				Name    string `json:"name"`
				URL     string `json:"url"`
				Snippet string `json:"snippet"`
			} `json:"value"`
		} `json:"webPages"`
	}

	if err := json.Unmarshal(body, &bingResponse); err != nil {
		return nil, err
	}

	results := make([]SearchResult, 0, len(bingResponse.WebPages.Value))
	for _, item := range bingResponse.WebPages.Value {
		results = append(results, SearchResult{
			Title:       item.Name,
			URL:         item.URL,
			Description: item.Snippet,
			Source:      "Bing",
		})
	}

	return results, nil
}

func ParseQueryFromInput(input map[string]any) string {
	query, _ := input["query"].(string)
	if query == "" {
		return ""
	}
	query = strings.TrimSpace(query)
	return query
}
