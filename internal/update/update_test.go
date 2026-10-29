package update

import (
	"testing"
)

func TestSemver(t *testing.T) {
	cases := []struct {
		input    string
		expected [3]int
	}{
		{"v1.2.3", [3]int{1, 2, 3}},
		{"1.0.0", [3]int{1, 0, 0}},
		{"v0.9.1-beta1", [3]int{0, 9, 1}},
		{"v2.1", [3]int{2, 1, 0}},
		{"invalid", [3]int{0, 0, 0}},
	}

	for _, tc := range cases {
		got := semver(tc.input)
		if got != tc.expected {
			t.Errorf("semver(%q) = %v; want %v", tc.input, got, tc.expected)
		}
	}
}

func TestIsNewer(t *testing.T) {
	cases := []struct {
		latest  string
		current string
		want    bool
	}{
		{"v1.2.0", "v1.1.0", true},
		{"v1.1.1", "v1.1.0", true},
		{"v2.0.0", "v1.9.9", true},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0.0", "v1.1.0", false},
		{"v1.0.0", "v2.0.0", false},
	}

	for _, tc := range cases {
		got := IsNewer(tc.latest, tc.current)
		if got != tc.want {
			t.Errorf("IsNewer(%q, %q) = %v; want %v", tc.latest, tc.current, got, tc.want)
		}
	}
}
