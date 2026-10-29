package adb

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"time"
)

// LogcatFilter describes a logcat session.
type LogcatFilter struct {
	Package    string // optional: filter by package pid
	Priority   string // V, D, I, W, E, F or "" for all
	Tag        string // optional tag filter
	ClearFirst bool
	DumpCrash  bool // dump crash buffer only
}

// RunLogcat streams logcat to the terminal with graceful Ctrl+C handling.
// The user stops it with Ctrl+C without killing the parent adbgo process.
func RunLogcat(serial string, f LogcatFilter) error {
	if f.ClearFirst {
		if _, err := Run(serial, "logcat", "-c"); err != nil {
			fmt.Println("warning: could not clear logcat buffer:", err)
		}
	}

	if f.DumpCrash {
		out, err := Run(serial, "logcat", "-b", "crash", "-d", "-v", "time")
		if err != nil {
			return err
		}
		if strings.TrimSpace(out) == "" {
			fmt.Println("No crash logs found in buffer.")
			return nil
		}
		fmt.Println(out)
		return nil
	}

	args := []string{"logcat", "-v", "time"}
	if f.Priority != "" {
		args = append(args, "*:"+f.Priority)
	}

	if f.Tag != "" {
		args = append(args, "-s", f.Tag)
	}

	if f.Package != "" {
		out, err := Run(serial, "shell", "pidof", "-s", f.Package)
		pid := strings.TrimSpace(out)
		if err == nil && pid != "" {
			args = append(args, "--pid="+pid)
			fmt.Printf("Filtering logs for %s (pid %s). Press Ctrl+C to stop.\n\n", f.Package, pid)
		} else {
			fmt.Printf("Note: %s is not currently running (no pid), streaming unfiltered logs. Press Ctrl+C to stop.\n\n", f.Package)
		}
	} else {
		fmt.Println("Streaming logcat. Press Ctrl+C to stop.")
	}

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	defer signal.Stop(sigChan)

	cmd := exec.Command("adb", append([]string{"-s", serial}, args...)...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start logcat: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-sigChan:
		fmt.Println("\nLogcat stopped.")
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(1 * time.Second):
			_ = cmd.Process.Kill()
		}
		return nil
	case err := <-done:
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 130 {
			return nil
		}
		if err != nil && strings.Contains(err.Error(), "signal: interrupt") {
			return nil
		}
		return err
	}
}
