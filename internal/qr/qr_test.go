package qr

import (
	"strings"
	"testing"
)

func TestEncodeChoosesSmallestVersion(t *testing.T) {
	cases := []struct {
		text string
		size int
	}{
		{"adbgo", 21},
		{strings.Repeat("x", 17), 21},
		{strings.Repeat("x", 18), 25},
		{strings.Repeat("x", 53), 29},
		{strings.Repeat("x", 78), 33},
		{strings.Repeat("x", 106), 37},
	}
	for _, c := range cases {
		m, err := Encode(c.text)
		if err != nil {
			t.Fatalf("Encode(%d bytes): %v", len(c.text), err)
		}
		if m.Size() != c.size {
			t.Errorf("Encode(%d bytes): size = %d, want %d", len(c.text), m.Size(), c.size)
		}
	}
}

func TestEncodeTooLong(t *testing.T) {
	if _, err := Encode(strings.Repeat("x", 107)); err == nil {
		t.Error("expected an error for 107 bytes")
	}
}

func TestFinderAndFunctionPatterns(t *testing.T) {
	m, err := Encode("adbgo")
	if err != nil {
		t.Fatal(err)
	}
	if !m[0][0] || !m[0][6] || !m[6][0] || !m[6][6] {
		t.Error("finder corners must be dark")
	}
	if m[0][7] || m[7][0] {
		t.Error("separator next to the finder must be light")
	}
	if !m[6][8] || m[6][9] {
		t.Error("timing pattern must alternate (dark at 8, light at 9)")
	}
	if !m[13][8] {
		t.Error("dark module at (8, 13) must be set")
	}
}

func TestAlignmentPattern(t *testing.T) {
	m, err := Encode(strings.Repeat("x", 20)) // version 2
	if err != nil {
		t.Fatal(err)
	}
	if !m[18][18] {
		t.Error("alignment center must be dark")
	}
	if m[17][18] {
		t.Error("alignment inner ring must be light")
	}
	if !m[16][18] {
		t.Error("alignment outer ring must be dark")
	}
}

func TestEncodeDeterministic(t *testing.T) {
	a, err := Encode("https://example.com/adbgo")
	if err != nil {
		t.Fatal(err)
	}
	b, err := Encode("https://example.com/adbgo")
	if err != nil {
		t.Fatal(err)
	}
	if a.Size() != b.Size() {
		t.Fatal("sizes differ")
	}
	for y := range a {
		for x := range a[y] {
			if a[y][x] != b[y][x] {
				t.Fatal("encode is not deterministic")
			}
		}
	}
}

func TestRenderANSI(t *testing.T) {
	m, err := Encode("adbgo")
	if err != nil {
		t.Fatal(err)
	}
	s := RenderANSI(m)
	if !strings.Contains(s, "▀") || !strings.Contains(s, "\x1b[") {
		t.Error("ANSI QR render must contain half blocks and escape codes")
	}
}
