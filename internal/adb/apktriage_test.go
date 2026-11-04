package adb

import (
	"strings"
	"testing"
)

func TestUTF16Strings(t *testing.T) {
	var b []byte
	for _, c := range "hello.permission.CAMERA" {
		b = append(b, byte(c), 0)
	}
	b = append(b, 0x12, 0x34)
	for _, c := range "ab" {
		b = append(b, byte(c), 0)
	}
	got := utf16Strings(b)
	if len(got) == 0 || got[0] != "hello.permission.CAMERA" {
		t.Errorf("unexpected strings: %v", got)
	}
	for _, s := range got {
		if s == "ab" {
			t.Error("short strings must be filtered")
		}
	}
}

func TestDangerousOf(t *testing.T) {
	d := dangerousOf([]string{
		"android.permission.CAMERA",
		"android.permission.INTERNET",
		"com.vendor.permission.CUSTOM_THING",
	})
	if len(d) != 1 || d[0] != "android.permission.CAMERA" {
		t.Errorf("unexpected dangerous set: %v", d)
	}
}

func TestFindSecrets(t *testing.T) {
	data := "key: AIza" + strings.Repeat("a", 35) +
		" aws AKIAIOSFODNN7EXAMPLE" +
		" gh ghp_" + strings.Repeat("b", 36) +
		" jwt eyJ" + strings.Repeat("x", 20) + "." + strings.Repeat("y", 20) + "." + strings.Repeat("z", 20) +
		" -----BEGIN RSA PRIVATE KEY-----"
	kinds := map[string]bool{}
	for _, f := range findSecrets(data) {
		kinds[f.Kind] = true
	}
	for _, want := range []string{"Google API key", "AWS access key", "GitHub token", "JWT", "Private key"} {
		if !kinds[want] {
			t.Errorf("missing finding %q in %v", want, kinds)
		}
	}
}
