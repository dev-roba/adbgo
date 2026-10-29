package adb

import (
	"testing"
)

func TestSanitize(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"192.168.1.50:5555", "192_168_1_50_5555"},
		{"Pixel-7_Pro", "Pixel-7_Pro"},
		{"device/with*bad?chars!", "device_with_bad_chars_"},
	}

	for _, tc := range cases {
		got := sanitize(tc.input)
		if got != tc.expected {
			t.Errorf("sanitize(%q) = %q; want %q", tc.input, got, tc.expected)
		}
	}
}

func TestIPRegex(t *testing.T) {
	cases := []struct {
		line  string
		match bool
		ip    string
	}{
		{"inet 192.168.1.100/24 brd", true, "192.168.1.100"},
		{"default via 10.0.0.1 dev wlan0 src 10.0.0.45", true, "10.0.0.1"},
		{"no ip here", false, ""},
	}

	for _, tc := range cases {
		m := ipRe.FindStringSubmatch(tc.line)
		if tc.match {
			if m == nil || m[1] != tc.ip {
				t.Errorf("ipRe failed on %q: got %v, want %q", tc.line, m, tc.ip)
			}
		} else {
			if m != nil {
				t.Errorf("ipRe unexpected match on %q: %v", tc.line, m)
			}
		}
	}
}
