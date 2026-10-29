package adb

import (
	"strconv"
	"strings"
)

// BatteryInfo holds parsed battery metrics.
type BatteryInfo struct {
	Level       int
	Scale       int
	Temperature float64 // in Celsius
	Voltage     float64 // in Volts
	Status      string
	Health      string
	PowerSource string
}

// GetBatteryInfo queries and parses `dumpsys battery`.
func GetBatteryInfo(serial string) (*BatteryInfo, error) {
	out, err := Run(serial, "shell", "dumpsys", "battery")
	if err != nil {
		return nil, err
	}

	info := &BatteryInfo{Scale: 100}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "level":
			info.Level, _ = strconv.Atoi(val)
		case "scale":
			info.Scale, _ = strconv.Atoi(val)
		case "temperature":
			tempRaw, _ := strconv.ParseFloat(val, 64)
			info.Temperature = tempRaw / 10.0 // dumpsys returns tenths of a degree C
		case "voltage":
			vRaw, _ := strconv.ParseFloat(val, 64)
			info.Voltage = vRaw / 1000.0 // dumpsys returns mV
		case "status":
			switch val {
			case "2":
				info.Status = "Charging"
			case "3":
				info.Status = "Discharging"
			case "4":
				info.Status = "Not charging"
			case "5":
				info.Status = "Full"
			default:
				info.Status = "Unknown (" + val + ")"
			}
		case "health":
			switch val {
			case "2":
				info.Health = "Good"
			case "3":
				info.Health = "Overheat"
			case "4":
				info.Health = "Dead"
			case "5":
				info.Health = "Over voltage"
			case "7":
				info.Health = "Cold"
			default:
				info.Health = "Normal"
			}
		case "AC powered":
			if val == "true" {
				info.PowerSource = "AC Charger"
			}
		case "USB powered":
			if val == "true" {
				info.PowerSource = "USB"
			}
		case "Wireless powered":
			if val == "true" {
				info.PowerSource = "Wireless"
			}
		}
	}
	if info.PowerSource == "" {
		info.PowerSource = "Battery"
	}
	return info, nil
}

// StorageInfo represents a filesystem usage row.
type StorageInfo struct {
	Filesystem string
	Size       string
	Used       string
	Avail      string
	UsePercent string
	MountedOn  string
}

// GetStorageInfo returns disk usage for main partitions.
func GetStorageInfo(serial string) ([]StorageInfo, error) {
	out, err := Run(serial, "shell", "df", "-h", "/data", "/system")
	if err != nil {
		return nil, err
	}
	var res []StorageInfo
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		if i == 0 || strings.TrimSpace(line) == "" {
			continue // skip header
		}
		fields := strings.Fields(line)
		if len(fields) >= 6 {
			res = append(res, StorageInfo{
				Filesystem: fields[0],
				Size:       fields[1],
				Used:       fields[2],
				Avail:      fields[3],
				UsePercent: fields[4],
				MountedOn:  fields[5],
			})
		}
	}
	return res, nil
}

// GetDisplaySpecs queries physical display size and density.
func GetDisplaySpecs(serial string) (size string, density string, err error) {
	sizeOut, _ := Run(serial, "shell", "wm", "size")
	densityOut, _ := Run(serial, "shell", "wm", "density")

	size = strings.TrimSpace(strings.ReplaceAll(sizeOut, "Physical size:", ""))
	density = strings.TrimSpace(strings.ReplaceAll(densityOut, "Physical density:", ""))
	return size, density, nil
}

// GetCurrentFocus finds the currently focused window / top activity.
func GetCurrentFocus(serial string) string {
	out, err := Run(serial, "shell", "dumpsys", "window")
	if err != nil {
		return "Unavailable"
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "mCurrentFocus") || strings.Contains(line, "mFocusedApp") {
			line = strings.TrimSpace(line)
			if idx := strings.Index(line, " "); idx != -1 {
				return line[idx+1:]
			}
			return line
		}
	}
	return "None / Home"
}

// OpenDeepLink sends an intent to open a URL or custom URI scheme.
func OpenDeepLink(serial, url string) error {
	_, err := Run(serial, "shell", "am", "start", "-a", "android.intent.action.VIEW", "-d", url)
	return err
}

// SendInputText types text into the focused Android text field with spaces properly escaped.
func SendInputText(serial, text string) error {
	// ADB input text treats spaces as literal spaces unless escaped or replaced with %s
	escaped := strings.ReplaceAll(text, " ", "%s")
	escaped = strings.ReplaceAll(escaped, "&", "\\&")
	escaped = strings.ReplaceAll(escaped, "\"", "\\\"")
	escaped = strings.ReplaceAll(escaped, "'", "\\'")
	_, err := Run(serial, "shell", "input", "text", escaped)
	return err
}

// EnableDemoMode configures a clean 100% battery, no notification status bar.
func EnableDemoMode(serial string) error {
	commands := [][]string{
		{"shell", "settings", "put", "global", "sysui_demo_allowed", "1"},
		{"shell", "am", "broadcast", "-a", "com.android.systemui.demo", "-e", "command", "enter"},
		{"shell", "am", "broadcast", "-a", "com.android.systemui.demo", "-e", "command", "clock", "-e", "hhmm", "1200"},
		{"shell", "am", "broadcast", "-a", "com.android.systemui.demo", "-e", "command", "battery", "-e", "level", "100", "-e", "plugged", "false"},
		{"shell", "am", "broadcast", "-a", "com.android.systemui.demo", "-e", "command", "network", "-e", "wifi", "show", "-e", "level", "4", "-e", "fully", "true"},
		{"shell", "am", "broadcast", "-a", "com.android.systemui.demo", "-e", "command", "notifications", "-e", "visible", "false"},
	}
	for _, cmd := range commands {
		if _, err := Run(serial, cmd...); err != nil {
			return err
		}
	}
	return nil
}

// DisableDemoMode restores the normal status bar.
func DisableDemoMode(serial string) error {
	_, err := Run(serial, "shell", "am", "broadcast", "-a", "com.android.systemui.demo", "-e", "command", "exit")
	return err
}
