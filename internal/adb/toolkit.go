package adb

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strings"
	"strconv"
	"time"
)

// RunContext executes one device-scoped ADB command with a timeout. It returns
// combined output and preserves the command error so callers cannot report false success.
func RunContext(ctx context.Context, serial string, args ...string) (string, error) {
	if strings.TrimSpace(serial) == "" {
		return "", fmt.Errorf("device serial is required")
	}
	fullArgs := append([]string{"-s", serial}, args...)
	cmd := exec.CommandContext(ctx, "adb", fullArgs...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("adb %s failed: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// ConnectTCPIP enables the legacy ADB-over-TCP mode on an already authorized
// USB-connected device, then connects to the supplied Wi-Fi address.
func ConnectTCPIP(ctx context.Context, serial, hostPort string) error {
	if strings.TrimSpace(serial) == "" {
		return fmt.Errorf("select a USB-connected device first")
	}
	host, portText, err := net.SplitHostPort(strings.TrimSpace(hostPort))
	if err != nil || host == "" || strings.ContainsAny(host, " \t\r\n/") {
		return fmt.Errorf("enter a valid phone address such as 192.168.1.20:5555")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("port must be a number from 1 to 65535")
	}
	if _, err := RunContext(ctx, serial, "tcpip", "5555"); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, "adb", "connect", hostPort)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb connect failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if !strings.Contains(strings.ToLower(string(out)), "connected to") && !strings.Contains(strings.ToLower(string(out)), "already connected") {
		return fmt.Errorf("ADB did not confirm connection: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

// InstallAPK installs an APK and returns ADB's output. Android package
// installation can replace an existing package; the caller should confirm first.
func InstallAPK(ctx context.Context, serial, path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("cannot access APK: %w", err)
	}
	if info.IsDir() || !strings.HasSuffix(strings.ToLower(path), ".apk") {
		return "", fmt.Errorf("choose a .apk file")
	}
	return RunContext(ctx, serial, "install", "-r", path)
}

// Push and Pull transfer a file and verify the local side where possible.
func Push(ctx context.Context, serial, localPath, remotePath string) (string, error) {
	if strings.TrimSpace(remotePath) == "" { return "", fmt.Errorf("remote destination path is required") }
	if _, err := os.Stat(localPath); err != nil {
		return "", fmt.Errorf("local source is unavailable: %w", err)
	}
	return RunContext(ctx, serial, "push", localPath, remotePath)
}
func Pull(ctx context.Context, serial, remotePath, localPath string) (string, error) {
	if strings.TrimSpace(remotePath) == "" || strings.TrimSpace(localPath) == "" { return "", fmt.Errorf("remote source and local destination paths are required") }
	out, err := RunContext(ctx, serial, "pull", remotePath, localPath)
	if err != nil { return out, err }
	if _, statErr := os.Stat(localPath); statErr != nil {
		return out, fmt.Errorf("ADB reported success but local destination is missing: %w", statErr)
	}
	return out, nil
}

// StreamLogcat streams log output until ctx is cancelled. If packageName is
// supplied, resolve its live PID and use logcat's PID filter instead of matching
// arbitrary text in each line.
func StreamLogcat(ctx context.Context, serial, priority, packageName string, dst io.Writer) error {
	if strings.TrimSpace(serial) == "" {
		return fmt.Errorf("device serial is required")
	}
	priority, err := NormalizePriority(priority)
	if err != nil {
		return err
	}
	args := []string{"-s", serial, "logcat"}
	if packageName = strings.TrimSpace(packageName); packageName != "" {
		if strings.ContainsAny(packageName, " \t\r\n") {
			return fmt.Errorf("package name must not contain whitespace")
		}
		pidOutput, pidErr := RunContext(ctx, serial, "shell", "pidof", packageName)
		if pidErr != nil {
			return fmt.Errorf("cannot resolve PID for %q; is the app running? %w", packageName, pidErr)
		}
		pids := strings.Fields(pidOutput)
		if len(pids) == 0 {
			return fmt.Errorf("no running process found for package %q", packageName)
		}
		for _, pid := range pids {
			if _, err := strconv.Atoi(pid); err != nil { return fmt.Errorf("device returned an invalid PID for %q", packageName) }
			args = append(args, "--pid="+pid)
		}
	}
	args = append(args, "*:"+priority)
	cmd := exec.CommandContext(ctx, "adb", args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil { return err }
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil { return err }
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 2*1024*1024)
	for scanner.Scan() {
		if _, err := fmt.Fprintln(dst, scanner.Text()); err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
		}
	}
	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	if ctx.Err() != nil { return nil }
	if scanErr != nil { return scanErr }
	if waitErr != nil { return fmt.Errorf("logcat exited: %w: %s", waitErr, strings.TrimSpace(stderr.String())) }
	return nil
}

// StartScreenRecording starts a bounded recording. The caller must stop the
// process and call Pull for the remote output; duration is capped at 3 minutes.
func StartScreenRecording(ctx context.Context, serial, remotePath string, seconds int) (*exec.Cmd, error) {
	if seconds < 1 || seconds > 180 { return nil, fmt.Errorf("recording duration must be between 1 and 180 seconds") }
	if !strings.HasPrefix(remotePath, "/sdcard/") { return nil, fmt.Errorf("recording path must be under /sdcard") }
	cmd := exec.CommandContext(ctx, "adb", "-s", serial, "shell", "screenrecord", "--time-limit", fmt.Sprint(seconds), remotePath)
	if err := cmd.Start(); err != nil { return nil, err }
	return cmd, nil
}

// DeviceShell starts a true interactive shell attached to the terminal.
func DeviceShell(serial string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.Command("adb", "-s", serial, "shell")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = stdin, stdout, stderr
	return cmd.Run()
}

var DefaultTimeout = 45 * time.Second
