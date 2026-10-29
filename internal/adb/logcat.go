package adb

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// LogcatFilter describes a logcat session.
type LogcatFilter struct {
	Package    string // optional: filter by package pid
	Priority   string // V, D, I, W, E, F or "" for all
	ClearFirst bool
}

// RunLogcat streams logcat to the terminal. Package filtering is done via
// the app's PID (with a fallback to raw streaming if the app is not running).
// The user stops it with Ctrl+C (SIGINT is delivered to the whole foreground
// process group, so the child adb process stops as well).
func RunLogcat(serial string, f LogcatFilter) error {
	if f.ClearFirst {
		if _, err := Run(serial, "logcat", "-c"); err != nil {
			fmt.Println("warning: could not clear logcat buffer:", err)
		}
	}

	args := []string{"logcat", "-v", "time"}
	if f.Priority != "" {
		args = append(args, "*:"+f.Priority)
	}

	if f.Package != "" {
		out, err := Run(serial, "shell", "pidof", "-s", f.Package)
		pid := strings.TrimSpace(out)
		if err == nil && pid != "" {
			args = append(args, "--pid="+pid)
			fmt.Printf("Filtering logs for %s (pid %s). Press Ctrl+C to stop.\n\n", f.Package, pid)
		} else {
			fmt.Printf("Note: %s is not running (no pid), streaming unfiltered logs. Press Ctrl+C to stop.\n\n", f.Package)
		}
	} else {
		fmt.Println("Streaming logcat. Press Ctrl+C to stop.\n")
	}

	cmd := exec.Command("adb", append([]string{"-s", serial}, args...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	// 130 = killed by SIGINT (Ctrl+C) — that is the normal way to stop logcat
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 130 {
		return nil
	}
	if err != nil && strings.Contains(err.Error(), "signal: interrupt") {
		return nil
	}
	return err
}
