package app

import (
	"testing"
)

func TestExtractJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "already JSON",
			input:    `{"key": "value"}`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "JSON with surrounding text",
			input:    `some text {"key": "value"} more text`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "JSON with newlines",
			input:    `some text\n{"key": "value"}\nmore text`,
			expected: `{"key": "value"}`,
		},
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "whitespace only",
			input:    "   ",
			expected: "",
		},
		{
			name:     "no JSON braces",
			input:    "some random text",
			expected: "some random text",
		},
		{
			name:     "multiple objects returns whole",
			input:    `{"first": 1} and {"second": 2}`,
			expected: `{"first": 1} and {"second": 2}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJSON(tt.input)
			if result != tt.expected {
				t.Errorf("extractJSON(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestHasDependencies(t *testing.T) {
	tests := []struct {
		name     string
		tasks    []coordinatorTask
		expected bool
	}{
		{
			name:     "no tasks",
			tasks:    []coordinatorTask{},
			expected: false,
		},
		{
			name: "task without dependencies",
			tasks: []coordinatorTask{
				{ID: "1", Role: "dev"},
			},
			expected: false,
		},
		{
			name: "task with empty depends_on",
			tasks: []coordinatorTask{
				{ID: "1", Role: "dev", DependsOn: []string{}},
			},
			expected: false,
		},
		{
			name: "task with dependencies",
			tasks: []coordinatorTask{
				{ID: "1", Role: "dev"},
				{ID: "2", Role: "test", DependsOn: []string{"1"}},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := hasDependencies(tt.tasks)
			if result != tt.expected {
				t.Errorf("hasDependencies() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestNormalizeCoordinatorRole(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"implementer", "implementer"},
		{"IMplementer", "implementer"},
		{"  IMPLEMENTER  ", "implementer"},
		{"", "implementer"},
		{"explorer", "explorer"},
		{"reviewer", "reviewer"},
		{"unknown_role", "unknown_role"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeCoordinatorRole(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeCoordinatorRole(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestShouldCoordinate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "short input under 12 words",
			input:    "hello world",
			expected: false,
		},
		{
			name:     "12 words without keywords",
			input:    "This is a simple task description that needs to be done now today please",
			expected: false,
		},
		{
			name:     "12 words with plan keyword",
			input:    "Please create a plan for implementing this great new feature today please do",
			expected: true,
		},
		{
			name:     "12 words with architecture keyword",
			input:    "Design the architecture for the new system that we are building today please",
			expected: true,
		},
		{
			name:     "12 words with review keyword",
			input:    "Review the code changes that were made in the last commit today please do",
			expected: true,
		},
		{
			name:     "newline separated with enough words",
			input:    "first step description here test second step description also here test please",
			expected: true,
		},
		{
			name:     "then conjunction with enough words",
			input:    "first step description then second step description also here test please do",
			expected: true,
		},
		{
			name:     "and conjunction with enough words",
			input:    "first step description here test and second step description also there test please",
			expected: true,
		},
		{
			name:     "also conjunction with enough words",
			input:    "first step description here test also second step description there too please do",
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := shouldCoordinate(tt.input)
			if result != tt.expected {
				t.Errorf("shouldCoordinate(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}
