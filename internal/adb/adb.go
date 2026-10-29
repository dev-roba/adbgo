package adb

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

type DeviceInfo struct {
	Serial  string
	Status  string // device, offline, unauthorized, etc.
	Model   string
	Product string
}

// ListDevices returns currently connected devices via `adb devices -l`
func ListDevices() ([]DeviceInfo, error) {
	cmd := exec.Command("adb", "devices", "-l")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("adb devices failed: %w\n%s", err, out.String())
	}

	var devices []DeviceInfo
	lines := strings.Split(out.String(), "\n")

	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "List of devices") {
			continue
		}

		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}

		serial := parts[0]
		status := parts[1]

		info := DeviceInfo{
			Serial: serial,
			Status: status,
		}

		// Parse extra fields like model:XXX product:YYY
		for _, p := range parts[2:] {
			if strings.HasPrefix(p, "model:") {
				info.Model = strings.TrimPrefix(p, "model:")
			}
			if strings.HasPrefix(p, "product:") {
				info.Product = strings.TrimPrefix(p, "product:")
			}
		}

		devices = append(devices, info)
	}

	return devices, nil
}

// IsConnected checks if a specific serial is currently online
func IsConnected(serial string) bool {
	devices, err := ListDevices()
	if err != nil {
		return false
	}
	for _, d := range devices {
		if d.Serial == serial && d.Status == "device" {
			return true
		}
	}
	return false
}

// Run runs an adb command for a specific device and returns combined output
func Run(serial string, args ...string) (string, error) {
	fullArgs := append([]string{"-s", serial}, args...)
	cmd := exec.Command("adb", fullArgs...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return out.String(), err
}

// RunInteractive runs adb command attached to current terminal (for shell, scrcpy, etc.)
func RunInteractive(serial string, args ...string) error {
	fullArgs := append([]string{"-s", serial}, args...)
	cmd := exec.Command("adb", fullArgs...)
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	// We let the command inherit the terminal for interactive use
	cmd.Stdin = nil
	return cmd.Run()
}

// StartScrcpy launches scrcpy for the device (blocking until scrcpy exits)
func StartScrcpy(serial string, extraArgs ...string) error {
	args := []string{"--serial", serial}
	args = append(args, extraArgs...)
	cmd := exec.Command("scrcpy", args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Reboot reboots the device into normal / recovery / bootloader
func Reboot(serial, mode string) error {
	args := []string{"reboot"}
	if mode != "" && mode != "normal" {
		args = []string{"reboot", mode}
	}
	_, err := Run(serial, args...)
	return err
}
