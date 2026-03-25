package app

import (
	"testing"
)

func TestNormalizeSessionID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "default"},
		{"   ", "default"},
		{"session-123", "session-123"},
		{"  session-456  ", "  session-456  "},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeSessionID(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeSessionID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsEmpty(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"", true},
		{"   ", true},
		{"hello", false},
		{"  hello  ", false},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isEmpty(tt.input)
			if result != tt.expected {
				t.Errorf("isEmpty(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestIsNotEmpty(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"", false},
		{"   ", false},
		{"hello", true},
		{"  hello  ", true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := isNotEmpty(tt.input)
			if result != tt.expected {
				t.Errorf("isNotEmpty(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestNormalizeString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"   ", ""},
		{"hello", "hello"},
		{"  hello  ", "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeString(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeString(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestContainsIgnoreCase(t *testing.T) {
	tests := []struct {
		s        string
		substr   string
		expected bool
	}{
		{"Hello World", "world", true},
		{"Hello World", "WORLD", true},
		{"Hello World", "foo", false},
		{"", "test", false},
		{"Hello", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.s+"-"+tt.substr, func(t *testing.T) {
			result := containsIgnoreCase(tt.s, tt.substr)
			if result != tt.expected {
				t.Errorf("containsIgnoreCase(%q, %q) = %v, want %v", tt.s, tt.substr, result, tt.expected)
			}
		})
	}
}

func TestEqualsIgnoreCase(t *testing.T) {
	tests := []struct {
		a        string
		b        string
		expected bool
	}{
		{"hello", "HELLO", true},
		{"Hello World", "hello world", true},
		{"hello", "world", false},
		{"", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.a+"-"+tt.b, func(t *testing.T) {
			result := equalsIgnoreCase(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("equalsIgnoreCase(%q, %q) = %v, want %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}
