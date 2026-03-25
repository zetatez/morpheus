package llm

import (
	"testing"

	"github.com/zetatez/morpheus/pkg/sdk"
)

func TestParseTextToolCalls(t *testing.T) {
	profile := ProviderProfile{
		TextToolCallRegex: `\{[^}]*tool_calls[^}]*\}`,
	}

	tests := []struct {
		name    string
		content string
		want    int
		wantErr bool
	}{
		{
			name:    "escaped newlines in tool_calls",
			content: `{"tool\ncalls":[{"name":"web\nfetch","arguments":{"url":"https://weather.com"}}]}`,
			want:    1,
			wantErr: false,
		},
		{
			name:    "simple tool_calls",
			content: `{"tool_calls":[{"name":"web_fetch","arguments":{"url":"https://example.com"}}]}`,
			want:    1,
			wantErr: false,
		},
		{
			name:    "nested arguments",
			content: `{"tool_calls":[{"name":"web_fetch","arguments":{"url":"https://example.com","method":"GET"}}]}`,
			want:    1,
			wantErr: false,
		},
		{
			name:    "multiple tool calls",
			content: `{"tool_calls":[{"name":"web_fetch","arguments":{"url":"https://a.com"}},{"name":"todo_write","arguments":{"text":"hello"}}]}`,
			want:    2,
			wantErr: false,
		},
		{
			name:    "camelCase tool name",
			content: `{"tool_calls":[{"name":"webFetch","arguments":{"url":"https://example.com"}}]}`,
			want:    1,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := profile.ParseTextToolCalls(tt.content)
			if len(calls) != tt.want {
				t.Errorf("ParseTextToolCalls() got %d calls, want %d. Content: %s", len(calls), tt.want, tt.content)
			}
			if tt.want > 0 && calls[0].Name == "" {
				t.Errorf("ParseTextToolCalls() got empty tool name")
			}
		})
	}
}

func TestNormalizeToolName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"web_fetch", "web_fetch"},
		{"web\nfetch", "web_fetch"},
		{"web\rfetch", "web_fetch"},
		{"web fetch", "web_fetch"},
		{"WEB_FETCH", "web_fetch"},
		{"Web_Fetch", "web_fetch"},
		{"WebFetch", "webfetch"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := sdk.NormalizeToolName(tt.input)
			if result != tt.expected {
				t.Errorf("sdk.NormalizeToolName(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeToolCallJSON(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`{"tool\ncalls":`, `{"tool_calls":`},
		{`"name":"web\nfetch"`, `"name":"web_fetch"`},
		{`"web fetch"`, `"web_fetch"`},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeToolCallJSON(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeToolCallJSON(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}
