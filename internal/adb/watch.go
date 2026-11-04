package adb

import (
	"os/exec"
	"strings"
	"time"
)

// NotifySend fires a desktop notification via notify-send when available.
func NotifySend(title, body string) {
	if _, err := exec.LookPath("notify-send"); err != nil {
		return
	}
	_ = exec.Command("notify-send", "-a", "adbgo", title, body).Start()
}

// AnyOnline reports whether at least one device is in 'device' state.
func AnyOnline() bool {
	devices, err := ListDevices()
	if err != nil {
		return false
	}
	for _, d := range devices {
		if d.Status == "device" {
			return true
		}
	}
	return false
}

// deviceOnlineWithReconnect checks a target and, for wireless targets,
// attempts one reconnect per call.
func deviceOnlineWithReconnect(target string) bool {
	if target == "" {
		return AnyOnline()
	}
	if strings.Contains(target, ":") && !IsConnected(target) {
		_ = ConnectDirect(target)
	}
	return IsConnected(target)
}

// Watchdog monitors a device (any device when target is empty), fires
// desktop notifications on connect/disconnect and auto-reconnects
// wireless endpoints with backoff. It blocks until stop is closed.
func Watchdog(stop <-chan struct{}, target string, onEvent func(event, msg string)) {
	online := false
	interval := 3 * time.Second
	for {
		now := deviceOnlineWithReconnect(target)
		if now != online {
			online = now
			interval = 3 * time.Second
			if now {
				NotifySend("adbgo watch", "📱 Phone connected")
				if onEvent != nil {
					onEvent("connect", target)
				}
			} else {
				NotifySend("adbgo watch", "📵 Phone left the network — did you forget it somewhere?")
				if onEvent != nil {
					onEvent("disconnect", target)
				}
			}
		}
		if !online && strings.Contains(target, ":") {
			if interval < 30*time.Second {
				interval *= 2
			}
		}
		select {
		case <-stop:
			return
		case <-time.After(interval):
		}
	}
}

// Radar watches mDNS and auto-connects wireless phones the moment they
// join the network. Blocks until stop is closed.
func Radar(stop <-chan struct{}, onEvent func(msg string)) {
	seen := map[string]bool{}
	for {
		targets, _ := DiscoverMDNSDetailed()
		for _, t := range targets {
			if seen[t.Addr] || IsConnected(t.Addr) {
				continue
			}
			if err := ConnectDirect(t.Addr); err == nil && IsConnected(t.Addr) {
				seen[t.Addr] = true
				model := modelOf(t.Addr)
				label := t.Instance
				if label == "" {
					label = model
				}
				msg := "🎯 " + label + " is home — connected (" + t.Addr + ")"
				NotifySend("adbgo radar", msg)
				if onEvent != nil {
					onEvent(msg)
				}
				rememberEndpoint(label, label, model, t.Addr)
			}
		}
		select {
		case <-stop:
			return
		case <-time.After(5 * time.Second):
		}
	}
}
