package adb

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"time"
)

// RecordScreen records the device screen with `screenrecord` in the
// background. When the user presses Ctrl+C, SIGINT is trapped so adbgo doesn't
// exit; the recording is pulled to local disk and removed from /sdcard.
func RecordScreen(serial string) (string, error) {
	timestamp := time.Now().Format("20060102-150405")
	remote := fmt.Sprintf("/sdcard/adbgo-rec-%s.mp4", timestamp)
	local := fmt.Sprintf("rec-%s-%s.mp4", sanitize(serial), timestamp)

	fmt.Println("Recording screen... press Ctrl+C to stop (max 3 minutes).")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt)
	defer signal.Stop(sigChan)

	cmd := exec.Command("adb", "-s", serial, "shell", "screenrecord", "--time-limit", "180", remote)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("screenrecord failed to start: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-sigChan:
		fmt.Println("\nStopping screen recording...")
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = cmd.Process.Kill()
		}
	case err := <-done:
		if err != nil && !strings.Contains(err.Error(), "signal: interrupt") {
			return "", fmt.Errorf("screenrecord failed: %w", err)
		}
	}

	// Give adbd a moment to finalize and flush the video file
	time.Sleep(800 * time.Millisecond)

	fmt.Println("Pulling recording to", local)
	if _, err := Run(serial, "pull", remote, local); err != nil {
		return "", fmt.Errorf("recording saved on device at %s but pull failed: %w", remote, err)
	}
	Run(serial, "shell", "rm", remote)
	return local, nil
}

// FileEntry is one entry of a remote directory listing.
type FileEntry struct {
	Name  string
	IsDir bool
	Size  string
}

// ListRemoteDir lists a directory on the device using `ls -1pa`
func ListRemoteDir(serial, path string) ([]FileEntry, error) {
	out, err := Run(serial, "shell", "ls", "-1p", path)
	if err != nil {
		return nil, err
	}
	var entries []FileEntry
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(strings.TrimSpace(line), "\r")
		if line == "" || line == "./" || line == "../" {
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

// ReadRemoteFile reads the beginning of a remote text file for previewing.
func ReadRemoteFile(serial, remotePath string, maxLines int) (string, error) {
	out, err := Run(serial, "shell", "head", "-n", fmt.Sprint(maxLines), remotePath)
	if err != nil {
		return "", err
	}
	return out, nil
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

// SyncPhotos pulls new photos from /sdcard/DCIM/Camera to local folder.
func SyncPhotos(serial, localDest string) (string, error) {
	if localDest == "" {
		localDest = fmt.Sprintf("photos-%s", sanitize(serial))
	}
	if err := os.MkdirAll(localDest, 0o755); err != nil {
		return "", err
	}
	// Pull only Camera directory
	remote := "/sdcard/DCIM/Camera"
	out, err := Run(serial, "pull", remote, filepath.Join(localDest, "Camera"))
	if err != nil {
		// Fallback to /sdcard/DCIM
		remote = "/sdcard/DCIM"
		out, err = Run(serial, "pull", remote, localDest)
		if err != nil {
			return out, err
		}
	}
	return localDest, nil
}
