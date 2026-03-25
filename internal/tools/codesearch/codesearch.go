package codesearch

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

type SearchBackend string

const (
	BackendSourcegraph SearchBackend = "sourcegraph"
	BackendSearchcode  SearchBackend = "searchcode"
	BackendGitHub      SearchBackend = "github"
)

type Tool struct {
	client   *http.Client
	backend  SearchBackend
	endpoint string
	apiKey   string
}

type CodeResult struct {
	Repository string  `json:"repository"`
	File       string  `json:"file"`
	LineNumber int     `json:"line_number"`
	Line       string  `json:"line"`
	Context    string  `json:"context,omitempty"`
	Language   string  `json:"language,omitempty"`
	Score      float64 `json:"score,omitempty"`
}

func NewTool(backend SearchBackend, endpoint, apiKey string) *Tool {
	return &Tool{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
		backend:  backend,
		endpoint: endpoint,
		apiKey:   apiKey,
	}
}

func (t *Tool) Name() string { return "codesearch" }

func (t *Tool) Describe() string { return "Search code repositories using semantic code search." }

func (t *Tool) Schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"query": map[string]any{
				"type":        "string",
				"description": "Code search query (supports regex, path:, lang:, repo: filters)",
			},
			"language": map[string]any{
				"type":        "string",
				"description": "Filter by programming language (e.g., go, python, javascript)",
			},
			"repo": map[string]any{
				"type":        "string",
				"description": "Filter by repository (e.g., github.com/user/repo)",
			},
			"max_results": map[string]any{
				"type":        "integer",
				"description": "Maximum number of results to return (default: 20)",
			},
			"context_lines": map[string]any{
				"type":        "integer",
				"description": "Number of context lines around matches (default: 2)",
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

	maxResults := 20
	if mr, ok := input["max_results"].(float64); ok {
		maxResults = int(mr)
	}

	contextLines := 2
	if cl, ok := input["context_lines"].(float64); ok {
		contextLines = int(cl)
	}

	language, _ := input["language"].(string)
	repo, _ := input["repo"].(string)

	var results []CodeResult
	var err error

	switch t.backend {
	case BackendSourcegraph:
		results, err = t.searchSourcegraph(ctx, query, language, repo, maxResults, contextLines)
	case BackendGitHub:
		results, err = t.searchGitHub(ctx, query, language, repo, maxResults)
	default:
		results, err = t.searchSearchcode(ctx, query, language, repo, maxResults)
	}

	if err != nil {
		return sdk.ToolResult{Success: false}, fmt.Errorf("code search failed: %w", err)
	}

	resultData := make([]map[string]any, 0, len(results))
	for _, r := range results {
		item := map[string]any{
			"repository":  r.Repository,
			"file":        r.File,
			"line":        r.Line,
			"line_number": r.LineNumber,
		}
		if r.Context != "" {
			item["context"] = r.Context
		}
		if r.Language != "" {
			item["language"] = r.Language
		}
		resultData = append(resultData, item)
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

func (t *Tool) searchSourcegraph(ctx context.Context, query, language, repo string, maxResults, contextLines int) ([]CodeResult, error) {
	if t.endpoint == "" {
		t.endpoint = "https://sourcegraph.com"
	}

	searchQuery := query
	if language != "" {
		searchQuery += fmt.Sprintf(" lang:%s", language)
	}
	if repo != "" {
		searchQuery += fmt.Sprintf(" repo:%s", repo)
	}

	apiURL := fmt.Sprintf("%s/.api/search", t.endpoint)
	params := url.Values{
		"q":      {searchQuery},
		"count":  {fmt.Sprintf("%d", maxResults)},
		"format": {"json"},
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if t.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+t.apiKey)
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

	var sgResponse struct {
		Results []struct {
			Repo     string `json:"repo"`
			File     string `json:"file"`
			Line     string `json:"line"`
			LineNum  int    `json:"lineNumber"`
			Context  string `json:"context"`
			Language string `json:"language"`
		} `json:"results"`
	}

	if err := json.Unmarshal(body, &sgResponse); err != nil {
		return nil, err
	}

	results := make([]CodeResult, 0, len(sgResponse.Results))
	for _, r := range sgResponse.Results {
		results = append(results, CodeResult{
			Repository: r.Repo,
			File:       r.File,
			Line:       r.Line,
			LineNumber: r.LineNum,
			Context:    r.Context,
			Language:   r.Language,
		})
	}

	return results, nil
}

func (t *Tool) searchSearchcode(ctx context.Context, query, language, repo string, maxResults int) ([]CodeResult, error) {
	apiURL := "https://searchcode.com/api/codesearch_I/"

	params := url.Values{
		"q":        {query},
		"per_page": {fmt.Sprintf("%d", maxResults)},
	}

	if language != "" {
		params.Set("lan", language)
	}

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Morpheus/1.0")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var scResponse []struct {
		Repository struct {
			Source string `json:"source"`
			Name   string `json:"name"`
		} `json:"repository"`
		Filename string            `json:"filename"`
		Lines    map[string]string `json:"lines"`
		Language string            `json:"language"`
	}

	if err := json.Unmarshal(body, &scResponse); err != nil {
		return nil, err
	}

	results := make([]CodeResult, 0)
	for _, r := range scResponse {
		repoName := r.Repository.Name
		if repoName == "" {
			repoName = r.Repository.Source
		}

		for lineNum, line := range r.Lines {
			var ln int
			fmt.Sscanf(lineNum, "%d", &ln)
			results = append(results, CodeResult{
				Repository: repoName,
				File:       r.Filename,
				Line:       line,
				LineNumber: ln,
				Language:   r.Language,
			})
		}
	}

	return results, nil
}

func (t *Tool) searchGitHub(ctx context.Context, query, language, repo string, maxResults int) ([]CodeResult, error) {
	if t.apiKey == "" {
		return nil, fmt.Errorf("GitHub code search requires an API token")
	}

	searchQuery := query
	if language != "" {
		searchQuery += fmt.Sprintf(" language:%s", language)
	}
	if repo != "" {
		searchQuery = fmt.Sprintf("repo:%s %s", repo, searchQuery)
	}

	apiURL := fmt.Sprintf("https://api.github.com/search/code?q=%s&per_page=%d", url.QueryEscape(searchQuery), maxResults)

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.text-match+json")
	req.Header.Set("Authorization", "Bearer "+t.apiKey)
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var ghResponse struct {
		Items []struct {
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
			Path        string `json:"path"`
			TextMatches []struct {
				Fragment string `json:"fragment"`
				Matches  []struct {
					Indices []int `json:"indices"`
				} `json:"matches"`
			} `json:"text_matches"`
		} `json:"items"`
	}

	if err := json.Unmarshal(body, &ghResponse); err != nil {
		return nil, err
	}

	results := make([]CodeResult, 0, len(ghResponse.Items))
	for _, item := range ghResponse.Items {
		for _, tm := range item.TextMatches {
			line := tm.Fragment
			if len(line) > 200 {
				line = line[:200] + "..."
			}
			results = append(results, CodeResult{
				Repository: item.Repository.FullName,
				File:       item.Path,
				Line:       line,
				LineNumber: 0,
			})
		}
	}

	return results, nil
}

func InferLanguage(filename string) string {
	ext := strings.ToLower(strings.TrimPrefix(filename, "."))
	langMap := map[string]string{
		"go":    "Go",
		"py":    "Python",
		"js":    "JavaScript",
		"ts":    "TypeScript",
		"tsx":   "TypeScript",
		"jsx":   "JavaScript",
		"java":  "Java",
		"rb":    "Ruby",
		"rs":    "Rust",
		"cpp":   "C++",
		"c":     "C",
		"h":     "C",
		"cs":    "C#",
		"php":   "PHP",
		"swift": "Swift",
		"kt":    "Kotlin",
		"scala": "Scala",
		"html":  "HTML",
		"css":   "CSS",
		"scss":  "SCSS",
		"json":  "JSON",
		"yaml":  "YAML",
		"yml":   "YAML",
		"xml":   "XML",
		"md":    "Markdown",
		"sql":   "SQL",
		"sh":    "Shell",
		"bash":  "Bash",
		"zsh":   "Zsh",
		"fish":  "Fish",
	}
	return langMap[ext]
}
