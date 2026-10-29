package adb

import (
	"bufio"
	"bytes"
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

// ReadLine reads a full line from stdin (unlike fmt.Scanln it handles spaces).
func ReadLine() string {
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
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

// Run runs an adb command for a specific device and returns combined output.
// A non-zero exit code is returned as an error that includes the output.
func Run(serial string, args ...string) (string, error) {
	fullArgs := append([]string{"-s", serial}, args...)
	cmd := exec.Command("adb", fullArgs...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("adb %s failed: %w\n%s", strings.Join(args, " "), err, out.String())
	}
	return out.String(), nil
}

// RunRaw runs an adb command without a target device (adb connect/disconnect,
// adb start-server, ...) and returns combined output.
func RunRaw(args ...string) (string, error) {
	cmd := exec.Command("adb", args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	if err != nil {
		return out.String(), fmt.Errorf("adb %s failed: %w\n%s", strings.Join(args, " "), err, out.String())
	}
	return out.String(), nil
}

// RunStreaming runs an adb command with stdin/stdout/stderr wired to the
// terminal so the user sees live output (installs, logcat, shells).
func RunStreaming(serial string, args ...string) error {
	fullArgs := append([]string{"-s", serial}, args...)
	cmd := exec.Command("adb", fullArgs...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
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

// Screenshot takes a screenshot and pulls it into the current directory.
func Screenshot(serial string) (string, error) {
	filename := fmt.Sprintf("screenshot-%s-%s.png", sanitize(serial), time.Now().Format("20060102-150405"))
	remote := "/sdcard/" + filename
	if _, err := Run(serial, "shell", "screencap", "-p", remote); err != nil {
		return "", err
	}
	if _, err := Run(serial, "pull", remote, filename); err != nil {
		return "", err
	}
	Run(serial, "shell", "rm", remote)
	return filename, nil
}

// WaitForFastboot polls `fastboot devices` until the serial appears or timeout.
func WaitForFastboot(serial string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := exec.Command("fastboot", "devices").Output()
		if err == nil && strings.Contains(string(out), serial) {
			return true
		}
		time.Sleep(2 * time.Second)
	}
	return false
}

// WakeScreen wakes the device screen
func WakeScreen(serial string) error {
	_, err := Run(serial, "shell", "input", "keyevent", "KEYCODE_WAKEUP")
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

// sanitize makes a serial safe to embed in a filename
func sanitize(s string) string {
	return strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			return r
		}
		return '_'
	}, s)
}
