package adb

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestPatternRules(t *testing.T) {
	if !patternValidNext([]int{1}, 2) {
		t.Error("1->2 should be valid")
	}
	if patternValidNext([]int{1}, 3) {
		t.Error("1->3 skipping unvisited 2 should be invalid")
	}
	if !patternValidNext([]int{1, 2}, 3) {
		t.Error("1->2->3 should be valid")
	}
	if !patternValidNext([]int{1}, 8) {
		t.Error("knight move 1->8 should be valid")
	}
	if patternValidNext([]int{1}, 9) {
		t.Error("1->9 skipping unvisited 5 should be invalid")
	}
	if !patternValidNext([]int{1, 5}, 9) {
		t.Error("1->5->9 should be valid")
	}
	if patternValidNext([]int{1, 2}, 1) {
		t.Error("revisiting a dot should be invalid")
	}
}

func countPatterns(minLen, maxLen int) int {
	s := newPatternStream(minLen, maxLen)
	n := 0
	for {
		_, ok := s.Next()
		if !ok {
			break
		}
		n++
	}
	return n
}

func TestPatternCounts(t *testing.T) {
	if got := countPatterns(3, 3); got != 320 {
		t.Errorf("patterns len 3 = %d, want 320", got)
	}
	if got := countPatterns(4, 4); got != 1624 {
		t.Errorf("patterns len 4 = %d, want 1624", got)
	}
}

func TestCharsetStream(t *testing.T) {
	s, err := newCharsetStream("01", "", 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for {
		c, ok := s.Next()
		if !ok {
			break
		}
		got = append(got, c)
	}
	want := []string{"0", "1", "00", "01", "10", "11"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestCharsetStreamMask(t *testing.T) {
	s, err := newCharsetStream("0123456789", "08??", 4, 4)
	if err != nil {
		t.Fatal(err)
	}
	first, ok := s.Next()
	if !ok || first != "0800" {
		t.Fatalf("first mask candidate = %q (ok=%v), want 0800", first, ok)
	}
	second, _ := s.Next()
	if second != "0801" {
		t.Fatalf("second mask candidate = %q, want 0801", second)
	}
}

func TestCommonPINsFirst(t *testing.T) {
	s, err := newCandidateStream(LockBruteConfig{Type: "pin", MinLen: 4, MaxLen: 4, CommonFirst: true})
	if err != nil {
		t.Fatal(err)
	}
	first, ok := s.Next()
	if !ok || first != "1234" {
		t.Fatalf("first pin candidate = %q (ok=%v), want 1234", first, ok)
	}
	seen := map[string]bool{"1234": true}
	dup := 0
	for i := 0; i < 200; i++ {
		c, ok := s.Next()
		if !ok {
			break
		}
		if seen[c] {
			dup++
		}
		seen[c] = true
	}
	if dup > 0 {
		t.Errorf("found %d duplicate candidates", dup)
	}
}

func TestPasswordVariants(t *testing.T) {
	v := passwordVariants("Password")
	expect := map[string]bool{"password": false, "PASSWORD": false, "Password": false, "password1": false, "Password123": false, "PASSWORD!": false}
	for _, cand := range v {
		if _, ok := expect[cand]; ok {
			expect[cand] = true
		}
	}
	for k, found := range expect {
		if !found {
			t.Errorf("missing variant %q", k)
		}
	}
}

func TestWordlistStream(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "words.txt")
	if err := os.WriteFile(p, []byte("# comment\n\nletmein\ntrustno1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s, err := newWordlistStream(p)
	if err != nil {
		t.Fatal(err)
	}
	first, ok := s.Next()
	if !ok || first != "letmein" {
		t.Fatalf("first wordlist candidate = %q (ok=%v), want letmein", first, ok)
	}
	n := 1
	for {
		_, ok := s.Next()
		if !ok {
			break
		}
		n++
	}
	// Two words, each with 3 unique case variants x len(pwSuffixes) suffixes.
	want := 2 * 3 * len(pwSuffixes)
	if n != want {
		t.Errorf("wordlist produced %d candidates, want %d", n, want)
	}
}

func TestParseWaitSeconds(t *testing.T) {
	if got := parseWaitSeconds("too many attempts, wait 30 seconds before trying again"); got != 30*time.Second {
		t.Errorf("parseWaitSeconds = %v, want 30s", got)
	}
	if got := parseWaitSeconds("try again in 5 secs"); got != 5*time.Second {
		t.Errorf("parseWaitSeconds = %v, want 5s", got)
	}
	if got := parseWaitSeconds("bad password, no delay"); got != 0 {
		t.Errorf("parseWaitSeconds = %v, want 0", got)
	}
}

func TestThrottleBackoff(t *testing.T) {
	if got := throttleBackoff(1); got != 30*time.Second {
		t.Errorf("throttleBackoff(1) = %v, want 30s", got)
	}
	if got := throttleBackoff(2); got != time.Minute {
		t.Errorf("throttleBackoff(2) = %v, want 1m", got)
	}
	if got := throttleBackoff(20); got != 15*time.Minute {
		t.Errorf("throttleBackoff(20) = %v, want 15m cap", got)
	}
}
