package adb

import "testing"

func TestSparkline(t *testing.T) {
	if s := sparkline([]int{10}); s != "" {
		t.Fatalf("single sample should render empty, got %q", s)
	}
	s := sparkline([]int{10, 20, 40, 80})
	if len([]rune(s)) != 4 {
		t.Fatalf("expected 4 blocks, got %q", s)
	}
	r := []rune(s)
	if r[0] == r[len(r)-1] {
		t.Fatalf("min and max samples should render different blocks, got %q", s)
	}
}
