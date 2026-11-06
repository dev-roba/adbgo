package adb

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// KnownEndpoint remembers where a phone was reachable last time, so
// SmartConnect can skip discovery on future runs.
type KnownEndpoint struct {
	Name  string `json:"name"`
	Model string `json:"model"`
	Addr  string `json:"addr"`
	When  string `json:"when"`
}

// knownFile is the persisted endpoint list (~/.config/adbgo/known.json).
type knownFile struct {
	Endpoints []KnownEndpoint `json:"endpoints"`
}

func knownFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "known.json"
	}
	return filepath.Join(home, ".config", "adbgo", "known.json")
}

// LoadKnown reads the saved endpoints (empty on first run).
func LoadKnown() *knownFile {
	kf := &knownFile{}
	data, err := os.ReadFile(knownFilePath())
	if err == nil {
		_ = json.Unmarshal(data, kf)
	}
	return kf
}

// Save persists the endpoint list.
func (k *knownFile) Save() error {
	if err := os.MkdirAll(filepath.Dir(knownFilePath()), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(k, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(knownFilePath(), data, 0o600)
}

// Remember upserts an endpoint by name.
func (k *knownFile) Remember(name, model, addr string) {
	when := time.Now().Format("2006-01-02 15:04")
	for i := range k.Endpoints {
		if k.Endpoints[i].Name == name {
			k.Endpoints[i].Model = model
			k.Endpoints[i].Addr = addr
			k.Endpoints[i].When = when
			return
		}
	}
	k.Endpoints = append(k.Endpoints, KnownEndpoint{Name: name, Model: model, Addr: addr, When: when})
}

// KnownEndpoints returns the saved endpoints for display and managers.
func KnownEndpoints() []KnownEndpoint {
	return LoadKnown().Endpoints
}

// ForgetEndpoint drops a saved endpoint by address and reports whether
// anything was removed.
func ForgetEndpoint(addr string) bool {
	kf := LoadKnown()
	var kept []KnownEndpoint
	removed := false
	for _, e := range kf.Endpoints {
		if e.Addr == addr {
			removed = true
			continue
		}
		kept = append(kept, e)
	}
	if removed {
		kf.Endpoints = kept
		_ = kf.Save()
	}
	return removed
}

// rememberEndpoint saves a successful endpoint for next time.
func rememberEndpoint(name, fallback, model, addr string) {
	if name == "" {
		name = fallback
	}
	if name == "" {
		name = model
	}
	if name == "" {
		name = addr
	}
	kf := LoadKnown()
	kf.Remember(name, model, addr)
	_ = kf.Save()
}

// modelOf returns the device model via getprop ("" on failure).
func modelOf(serial string) string {
	out, err := Run(serial, "shell", "getprop", "ro.product.model")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// instanceMatches checks an mDNS instance name against the wanted name
// plus aliases from saved endpoints (name ↔ model).
func instanceMatches(want, instance string) bool {
	if strings.Contains(strings.ToLower(instance), want) {
		return true
	}
	inst := strings.ToLower(instance)
	for _, e := range LoadKnown().Endpoints {
		if strings.EqualFold(want, e.Name) && strings.Contains(inst, strings.ToLower(e.Model)) {
			return true
		}
		if strings.EqualFold(want, e.Model) && strings.Contains(inst, strings.ToLower(e.Name)) {
			return true
		}
	}
	return false
}

// tryMDNSChain attempts each advertised target. With a name filter it
// also connects and verifies the model, because mDNS instance names are
// random strings that never contain a phone's model.
func tryMDNSChain(targets []MDNSTarget, want string, say func(string)) (string, string, bool) {
	for _, t := range targets {
		if t.Addr == "" {
			continue
		}
		if want != "" && !instanceMatches(want, t.Instance) {
			// instance names are random — connect and check the model
			if err := ConnectDirect(t.Addr); err != nil || !IsConnected(t.Addr) {
				continue
			}
			model := modelOf(t.Addr)
			if !strings.Contains(strings.ToLower(model), want) {
				continue
			}
			say("hop 2 (mDNS): " + model + " answers as '" + t.Instance + "' at " + t.Addr)
			rememberEndpoint(want, model, model, t.Addr)
			return t.Addr, "mDNS", true
		}
		if err := ConnectDirect(t.Addr); err != nil || !IsConnected(t.Addr) {
			continue
		}
		model := modelOf(t.Addr)
		say("hop 2 (mDNS): " + t.Instance + " at " + t.Addr)
		rememberEndpoint(want, t.Instance, model, t.Addr)
		return t.Addr, "mDNS", true
	}
	return "", "", false
}

// SmartConnect runs the full priority chain — USB → mDNS (adb backend +
// raw multicast fallback) → saved endpoints → LAN sweep for port 5555 —
// and reports which hop worked. It only touches networks this machine
// sits on.
func SmartConnect(name string, verbose func(string)) (string, string, error) {
	say := func(msg string) {
		if verbose != nil {
			verbose(msg)
		}
	}
	want := strings.ToLower(strings.TrimSpace(name))

	// Hop 1: USB.
	devices, _ := ListDevices()
	for _, d := range devices {
		if d.Status != "device" || strings.Contains(d.Serial, ":") {
			continue
		}
		if want == "" || strings.Contains(strings.ToLower(d.Model), want) || strings.Contains(strings.ToLower(d.Product), want) {
			say("hop 1 (USB): " + d.Serial + " (" + d.Model + ")")
			return d.Serial, "USB", nil
		}
	}

	// Hop 2: mDNS — adb backend first, raw multicast as fallback.
	if serial, how, ok := tryMDNSChain(DiscoverMDNSSafe(), want, say); ok {
		return serial, how, nil
	}
	if serial, how, ok := tryMDNSChain(QueryMDNSService(MDNSConnectService, 2*time.Second), want, say); ok {
		return serial, how, nil
	}

	// Hop 3: saved endpoints from ~/.config/adbgo/known.json.
	kf := LoadKnown()
	for _, e := range kf.Endpoints {
		if want != "" && !strings.EqualFold(want, e.Name) && !strings.Contains(strings.ToLower(e.Model), want) && !strings.Contains(strings.ToLower(e.Addr), want) {
			continue
		}
		say("hop 3 (saved): trying " + e.Name + " at " + e.Addr)
		if err := ConnectDirect(e.Addr); err == nil && IsConnected(e.Addr) {
			rememberEndpoint(e.Name, e.Name, e.Model, e.Addr) // refresh last-seen
			return e.Addr, "saved endpoint", nil
		}
	}

	// Hop 4: sweep the LAN for adbd on port 5555.
	say("hop 4: sweeping the LAN for port 5555...")
	open, _ := ScanLANPorts(5555, nil)
	for _, addr := range open {
		if err := ConnectDirect(addr); err == nil && IsConnected(addr) {
			model := modelOf(addr)
			say("hop 4 (LAN sweep): " + addr + " (" + model + ")")
			rememberEndpoint(want, model, model, addr)
			return addr, "LAN sweep", nil
		}
	}

	return "", "", fmt.Errorf("no reachable phone found (tried USB, mDNS incl. raw multicast, %d saved endpoint(s), LAN sweep) — a phone using wireless debugging? Run `adbgo pairqr` or `adbgo lan deep <ip>`", len(kf.Endpoints))
}
