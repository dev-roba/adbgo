package adb

import (
	"fmt"
	"math/rand"
	"strings"
	"time"
)

// GeneratePairQR invents a pairing service name and a 6-digit code using
// the same scheme Android Studio does: the phone scans the QR (a
// WIFI:T:ADB payload), trusts the code it contains, and then advertises
// its pairing service over mDNS for us to dial.
func GeneratePairQR() (payload, name, code string) {
	name = fmt.Sprintf("adbgo-%04x%04x", rand.Intn(0x10000), rand.Intn(0x10000))
	code = fmt.Sprintf("%06d", 100000+rand.Intn(900000))
	payload = "WIFI:T:ADB;S:" + name + ";P:" + code + ";;"
	return payload, name, code
}

// MDNSTarget is one adb service advertisement from `adb mdns services`.
type MDNSTarget struct {
	Instance string
	Addr     string
}

// DiscoverMDNSDetailed parses `adb mdns services` into instance + address
// pairs for adb pairing/connect services.
func DiscoverMDNSDetailed() ([]MDNSTarget, error) {
	out, err := RunRaw("mdns", "services")
	if err != nil {
		return nil, err
	}
	var targets []MDNSTarget
	seen := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "_adb") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		addr := parts[len(parts)-1]
		instance := parts[1]
		if i := strings.Index(instance, "._adb"); i > 0 {
			instance = instance[:i]
		}
		if seen[addr] {
			continue
		}
		seen[addr] = true
		targets = append(targets, MDNSTarget{Instance: instance, Addr: addr})
	}
	return targets, nil
}

// WaitForPairingService polls mDNS for the phone's pairing service after
// it scanned the QR. It uses the adb backend first and falls back to a
// raw multicast query each round, preferring the instance matching our
// service name but accepting any _adb-tls-pairing advertisement.
func WaitForPairingService(name string, timeout time.Duration, tick func(elapsed int)) (string, error) {
	start := time.Now()
	deadline := start.Add(timeout)
	for time.Now().Before(deadline) {
		if tick != nil {
			tick(int(time.Since(start).Seconds()))
		}
		if addr, err := pairingServiceFromADB(name); err == nil {
			return addr, nil
		}
		if addr, err := FindPairingService(name, 2*time.Second); err == nil {
			return addr, nil
		}
		time.Sleep(1 * time.Second)
	}
	return "", fmt.Errorf("no pairing service appeared within %s — make sure the phone scanned the QR and is on the same Wi-Fi", timeout)
}

// pairingServiceFromADB looks for the pairing service via `adb mdns services`.
func pairingServiceFromADB(name string) (string, error) {
	out, _ := RunRaw("mdns", "services")
	fallback := ""
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "_adb-tls-pairing._tcp") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		addr := parts[len(parts)-1]
		if fallback == "" {
			fallback = addr
		}
		if strings.Contains(parts[1], name) {
			return addr, nil
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("pairing service not found via adb mdns")
}

// PairWithAddr runs `adb pair` against the phone's pairing service using
// the code that was encoded in the QR.
func PairWithAddr(addr, code string) (string, error) {
	out, err := RunRaw("pair", addr, code)
	if err != nil {
		return out, err
	}
	if strings.Contains(out, "Failed") {
		return out, fmt.Errorf("pairing failed: %s", strings.TrimSpace(out))
	}
	return out, nil
}
