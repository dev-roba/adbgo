package adb

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
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
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	return RunContext(ctx, serial, args...)
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

// WakeScreen wakes the device screen
func WakeScreen(serial string) error {
	_, err := Run(serial, "shell", "input", "keyevent", "KEYCODE_WAKEUP")
	return err
}

// SwipeUp performs a swipe up gesture (to reveal lock screen)
func SwipeUp(serial string) error {
	// Generic swipe from bottom to middle (works on most phones)
	_, err := Run(serial, "shell", "input", "swipe", "300", "1500", "300", "800", "300")
	return err
}

// UnlockWithPassword wakes, swipes, types the password and presses Enter
func UnlockWithPassword(serial, password string) error {
	// 1. Wake
	if err := WakeScreen(serial); err != nil {
		return err
	}
	time.Sleep(400 * time.Millisecond)

	// 2. Swipe up
	if err := SwipeUp(serial); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)

	// 3. Type password
	_, err := Run(serial, "shell", "input", "text", password)
	if err != nil {
		return err
	}
	time.Sleep(200 * time.Millisecond)

	// 4. Press Enter
	_, err = Run(serial, "shell", "input", "keyevent", "66") // KEYCODE_ENTER
	return err
}

// LockScreen locks the device
func LockScreen(serial string) error {
	// Prefer KEYCODE_SLEEP, fallback to POWER
	_, err := Run(serial, "shell", "input", "keyevent", "KEYCODE_SLEEP")
	if err != nil {
		_, err = Run(serial, "shell", "input", "keyevent", "26") // POWER
	}
	return err
}
