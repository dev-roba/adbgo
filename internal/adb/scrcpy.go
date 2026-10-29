package adb

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/dev-roba/adbgo/internal/config"
)

// ScrcpyProfile represents a preset configuration for scrcpy.
type ScrcpyProfile struct {
	Name          string
	TurnScreenOff bool
	StayAwake     bool
	AlwaysOnTop   bool
	Borderless    bool
	Fullscreen    bool
	NoAudio       bool
	AudioDup      bool
	MaxFPS        int
	MaxSize       int
	BitRateM      int
	Codec         string // h264, h265, av1
	RecordToPC    bool
	ExtraArgs     []string
}

// BuildArgs converts the profile into command-line arguments for scrcpy.
func (p *ScrcpyProfile) BuildArgs(serial string) ([]string, string) {
	args := []string{"--serial", serial}

	if p.TurnScreenOff {
		args = append(args, "--turn-screen-off")
	}
	if p.StayAwake {
		args = append(args, "--stay-awake")
	}
	if p.AlwaysOnTop {
		args = append(args, "--always-on-top")
	}
	if p.Borderless {
		args = append(args, "--window-borderless")
	}
	if p.Fullscreen {
		args = append(args, "--fullscreen")
	}
	if p.NoAudio {
		args = append(args, "--no-audio")
	} else if p.AudioDup {
		args = append(args, "--audio-dup")
	}
	if p.MaxFPS > 0 {
		args = append(args, fmt.Sprintf("--max-fps=%d", p.MaxFPS))
	}
	if p.MaxSize > 0 {
		args = append(args, fmt.Sprintf("--max-size=%d", p.MaxSize))
	}
	if p.BitRateM > 0 {
		args = append(args, fmt.Sprintf("--video-bit-rate=%dM", p.BitRateM))
	}
	if p.Codec != "" {
		args = append(args, fmt.Sprintf("--video-codec=%s", p.Codec))
	}

	var recordFile string
	if p.RecordToPC {
		recordFile = fmt.Sprintf("scrcpy-rec-%s-%s.mp4", sanitize(serial), time.Now().Format("20060102-150405"))
		args = append(args, "--record="+recordFile)
	}

	args = append(args, p.ExtraArgs...)
	return args, recordFile
}

// LaunchScrcpyClean runs scrcpy with all stdout/stderr redirected to a background log
// file so the terminal UI stays completely clean without spamming ffmpeg/scrcpy logs.
func LaunchScrcpyClean(serial string, profile ScrcpyProfile) (recordSaved string, err error) {
	confDir, err := config.ConfigDir()
	if err != nil {
		confDir = os.TempDir()
	}
	logPath := filepath.Join(confDir, "scrcpy.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return "", fmt.Errorf("could not create log file: %w", err)
	}
	defer logFile.Close()

	args, recordFile := profile.BuildArgs(serial)
	cmd := exec.Command("scrcpy", args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("failed to start scrcpy: %w", err)
	}

	// Wait for scrcpy window to close
	if err := cmd.Wait(); err != nil {
		// Read last 6 lines of log to explain why it failed
		tail := readLogTail(logPath, 6)
		if tail != "" {
			return "", fmt.Errorf("scrcpy exited with error:\n%s", tail)
		}
		return "", fmt.Errorf("scrcpy error: %w", err)
	}

	return recordFile, nil
}

func readLogTail(path string, lines int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()

	var collected []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		collected = append(collected, scanner.Text())
		if len(collected) > lines {
			collected = collected[1:]
		}
	}
	return strings.Join(collected, "\n")
}
