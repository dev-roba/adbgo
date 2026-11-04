package adb

import (
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	healthMu      sync.Mutex
	healthSamples []int
)

var rssiRe = regexp.MustCompile(`(?i)rssi[^0-9\n-]*(-?\d+)`)

// PingSerial measures one adb shell round-trip.
func PingSerial(serial string) (time.Duration, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	start := time.Now()
	cmd := exec.CommandContext(ctx, "adb", "-s", serial, "shell", "echo", "ok")
	if err := cmd.Run(); err != nil {
		return 0, err
	}
	return time.Since(start), nil
}

// GetRSSI returns the phone's current Wi-Fi signal strength in dBm.
func GetRSSI(serial string) (int, error) {
	out, err := Run(serial, "shell", "cmd", "wifi", "status")
	if err != nil {
		out, err = Run(serial, "shell", "dumpsys", "wifi")
		if err != nil {
			return 0, err
		}
	}
	if m := rssiRe.FindStringSubmatch(out); m != nil {
		if r, serr := strconv.Atoi(m[1]); serr == nil {
			return r, nil
		}
	}
	return 0, fmt.Errorf("no RSSI reported by the device")
}

// sparkline renders recent latency samples as a mini bar chart.
func sparkline(samples []int) string {
	blocks := []rune("▁▂▃▄▅▆▇█")
	if len(samples) < 2 {
		return ""
	}
	n := len(samples)
	if n > 12 {
		n = 12
	}
	s := samples[len(samples)-n:]
	max := 1
	for _, v := range s {
		if v > max {
			max = v
		}
	}
	var b strings.Builder
	for _, v := range s {
		b.WriteRune(blocks[v*(len(blocks)-1)/max])
	}
	return b.String()
}

// HealthLine returns a one-line connection status for the main menu:
// transport badge, adb latency, Wi-Fi RSSI and a latency sparkline.
// It returns "" when the device is unreachable.
func HealthLine(serial string) string {
	if serial == "" {
		return ""
	}
	lat, err := PingSerial(serial)
	if err != nil {
		return ""
	}
	healthMu.Lock()
	healthSamples = append(healthSamples, int(lat.Milliseconds()))
	if len(healthSamples) > 20 {
		healthSamples = healthSamples[len(healthSamples)-20:]
	}
	spark := sparkline(healthSamples)
	healthMu.Unlock()

	transport := "🔌 USB"
	if strings.Contains(serial, ":") {
		transport = "📶 Wi-Fi"
	}
	line := fmt.Sprintf("  %s · ⏱ %dms", transport, lat.Milliseconds())
	if strings.Contains(serial, ":") {
		if rssi, rerr := GetRSSI(serial); rerr == nil {
			quality := "weak"
			if rssi > -55 {
				quality = "great"
			} else if rssi > -67 {
				quality = "good"
			} else if rssi > -75 {
				quality = "ok"
			}
			line += fmt.Sprintf(" · 📡 %d dBm (%s)", rssi, quality)
		}
	}
	if spark != "" {
		line += " · " + spark
	}
	return line
}
