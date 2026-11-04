package adb

import (
	"strings"
	"testing"
)

func TestGeneratePairQR(t *testing.T) {
	payload, name, code := GeneratePairQR()
	if !strings.HasPrefix(payload, "WIFI:T:ADB;S:") {
		t.Fatalf("bad payload: %s", payload)
	}
	if !strings.Contains(payload, ";P:"+code+";;") {
		t.Fatalf("payload does not contain the code: %s", payload)
	}
	if !strings.HasPrefix(name, "adbgo-") {
		t.Fatalf("bad service name: %s", name)
	}
	if len(code) != 6 {
		t.Fatalf("code must be 6 digits, got %q", code)
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			t.Fatalf("code must be numeric, got %q", code)
		}
	}
}
