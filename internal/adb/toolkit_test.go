package adb

import "testing"

func TestNormalizePriority(t *testing.T) {
	tests := []struct {
		input string
		want string
		bad bool
	}{
		{"", "V", false},
		{"v", "V", false},
		{" e ", "E", false},
		{"W", "W", false},
		{"x", "", true},
		{"verbose", "", true},
	}
	for _, tt := range tests {
		got, err := NormalizePriority(tt.input)
		if tt.bad && err == nil { t.Errorf("NormalizePriority(%q) expected error", tt.input); continue }
		if !tt.bad && err != nil { t.Errorf("NormalizePriority(%q): %v", tt.input, err); continue }
		if got != tt.want { t.Errorf("NormalizePriority(%q) = %q, want %q", tt.input, got, tt.want) }
	}
}
