package adb

import (
	"strings"
	"testing"
)

func TestParseGetprop(t *testing.T) {
	out := "[ro.product.brand]: [Google]\n[ro.build.version.release]: [14]\n[ro.product.model]: [Pixel 8]\n"
	m := ParseGetprop(out)
	if m["ro.product.brand"] != "Google" || m["ro.build.version.release"] != "14" || m["ro.product.model"] != "Pixel 8" {
		t.Errorf("unexpected map: %v", m)
	}
}

func TestPatchAge(t *testing.T) {
	f := &Fingerprint{SecurityPatch: "2020-01-01"}
	age, ok := f.PatchAge()
	if !ok || age <= 0 {
		t.Errorf("PatchAge = %v, %v; want a parsed past date", age, ok)
	}
	bad := &Fingerprint{SecurityPatch: "bogus"}
	if _, ok := bad.PatchAge(); ok {
		t.Error("PatchAge should fail on a bogus date")
	}
}

func TestRiskVerdict(t *testing.T) {
	hardened := &Fingerprint{SELinux: "Enforcing", VerifiedBoot: "green", EncryptionState: "encrypted"}
	if v := hardened.RiskVerdict(); !strings.Contains(v, "LOW") {
		t.Errorf("hardened verdict = %q, want LOW", v)
	}
	risky := &Fingerprint{
		SELinux:         "Permissive",
		Rooted:          true,
		RootIndicators:  []string{"su binary at /system/bin/su"},
		VerifiedBoot:    "orange",
		EncryptionState: "encrypted",
	}
	v := risky.RiskVerdict()
	if !strings.Contains(v, "SELinux") || !strings.Contains(v, "root") || !strings.Contains(v, "ELEVATED") {
		t.Errorf("risky verdict = %q, want ELEVATED with SELinux & root", v)
	}
}
