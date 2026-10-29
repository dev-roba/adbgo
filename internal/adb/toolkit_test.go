package adb

import (
	"context"
	"testing"
)

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

func TestRunContextRequiresSerial(t *testing.T) {
	if _, err := RunContext(context.Background(), " ", "devices"); err == nil {
		t.Fatal("RunContext accepted an empty device serial")
	}
}

func TestWirelessEndpointValidation(t *testing.T) {
	if err := validateEndpoint("192.168.1.2:5555"); err != nil {
		t.Fatalf("valid endpoint rejected: %v", err)
	}
	for _, endpoint := range []string{"", "192.168.1.2", "host:0", "host:70000", "bad host:5555"} {
		if err := validateEndpoint(endpoint); err == nil {
			t.Errorf("validateEndpoint(%q) expected error", endpoint)
		}
	}
}

func TestPairWirelessRejectsInvalidInputBeforeCallingADB(t *testing.T) {
	err := PairWireless(context.Background(), "192.168.1.2:37123", "12ab56", "192.168.1.2:5555")
	if err == nil {
		t.Fatal("PairWireless accepted a non-numeric pairing code")
	}
	err = PairWireless(context.Background(), "192.168.1.2:37123", "123", "192.168.1.2:5555")
	if err == nil {
		t.Fatal("PairWireless accepted a pairing code with the wrong length")
	}
}
