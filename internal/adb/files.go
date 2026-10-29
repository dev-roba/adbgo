package adb

import (
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// RecordScreen records the device screen with `screenrecord` in the
// background. When the user presses Ctrl+C the recording is pulled from the
// device into the current directory and removed from /sdcard.
func RecordScreen(serial string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	remote := fmt.Sprintf("/sdcard/adbgo-rec-%s.mp4", timestamp)
	local := fmt.Sprintf("rec-%s.mp4", timestamp)

	fmt.Println("Recording... press Ctrl+C to stop (max 3 minutes).")

	cmd := exec.Command("adb", "-s", serial, "shell", "screenrecord", "--time-limit", "180", remote)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	err := cmd.Run()
	// Ctrl+C kills screenrecord too — treat interrupt as a normal stop
	if err != nil && !strings.Contains(err.Error(), "signal: interrupt") {
		return "", fmt.Errorf("screenrecord failed: %w", err)
	}

	// Give adbd a moment to close the file
	time.Sleep(500 * time.Millisecond)

	fmt.Println("Pulling recording to", local)
	if _, err := Run(serial, "pull", remote, local); err != nil {
		return "", fmt.Errorf("recording saved on device at %s but the pull failed: %w", remote, err)
	}
	Run(serial, "shell", "rm", remote)
	return local, nil
}

// FileEntry is one entry of a remote directory listing.
type FileEntry struct {
	Name  string
	IsDir bool
}

// ListRemoteDir lists a directory on the device using `ls -1p`
// (directories end with "/").
func ListRemoteDir(serial, path string) ([]FileEntry, error) {
	out, err := Run(serial, "shell", "ls", "-1p", path)
	if err != nil {
		return nil, err
	}
	var entries []FileEntry
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(strings.TrimSpace(line), "\r")
		if line == "" {
			continue
		}
		isDir := strings.HasSuffix(line, "/")
		name := strings.TrimSuffix(line, "/")
		if name == "" {
			continue
		}
		entries = append(entries, FileEntry{Name: name, IsDir: isDir})
	}
	return entries, nil
}

// Pull downloads a remote file/folder into the current directory.
func Pull(serial, remotePath string) (string, error) {
	parts := strings.Split(strings.TrimSuffix(remotePath, "/"), "/")
	local := parts[len(parts)-1]
	if local == "" || local == "." || local == ".." {
		return "", fmt.Errorf("invalid remote path: %s", remotePath)
	}
	if _, err := Run(serial, "pull", remotePath, local); err != nil {
		return "", err
	}
	return local, nil
}

// Push uploads a local file/folder to a remote directory.
func Push(serial, localPath, remoteDir string) error {
	_, err := Run(serial, "push", localPath, strings.TrimSuffix(remoteDir, "/")+"/")
	return err
}

// RemoteDelete removes a file/folder on the device (rm -rf — dangerous).
func RemoteDelete(serial, remotePath string) error {
	_, err := Run(serial, "shell", "rm", "-rf", remotePath)
	return err
}

// RemoteMkdir creates a directory on the device.
func RemoteMkdir(serial, remotePath string) error {
	_, err := Run(serial, "shell", "mkdir", "-p", remotePath)
	return err
}

// InstallAPK installs an APK (or an .apkm/.apks bundle via --adb) with
// streamed output so the user sees live progress.
func InstallAPK(serial, apkPath string, allowDowngrade bool) error {
	args := []string{"install", "-r"}
	if allowDowngrade {
		args = append(args, "-d")
	}
	args = append(args, apkPath)
	return RunStreaming(serial, args...)
}

// UserPackages returns the list of installed third-party package names.
func UserPackages(serial string) ([]string, error) {
	out, err := Run(serial, "shell", "pm", "list", "packages", "-3")
	if err != nil {
		return nil, err
	}
	var pkgs []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "package:") {
			pkgs = append(pkgs, strings.TrimPrefix(line, "package:"))
		}
	}
	return pkgs, nil
}
