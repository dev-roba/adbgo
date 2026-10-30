package adb

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// photoExts lists media file extensions treated as photos or recordings.
var photoExts = map[string]bool{
	".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
	".heic": true, ".gif": true, ".bmp": true, ".dng": true,
	".mp4": true, ".3gp": true, ".mkv": true,
}

// IsMediaFile reports whether a filename looks like a photo or video.
func IsMediaFile(name string) bool {
	return photoExts[strings.ToLower(filepath.Ext(name))]
}

// ListRemoteMedia recursively lists media file paths under remoteDir.
func ListRemoteMedia(serial, remoteDir string) ([]string, error) {
	out, err := Run(serial, "shell", "find", remoteDir, "-type", "f")
	if err != nil {
		return nil, fmt.Errorf("cannot list %s: %s", remoteDir, err.Error())
	}
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" && IsMediaFile(line) {
			files = append(files, line)
		}
	}
	return files, nil
}

// PullPhotos copies (move=false) or cuts (move=true) every media file
// under remoteDir into localDest, preserving the remote directory
// structure. progress (optional) is called after each file. It returns
// the number of files pulled, the number deleted from the device (only
// when move is true and both pull and delete succeeded) and the number
// of failures. Files are only deleted after their pull succeeded, so a
// cut never loses data.
func PullPhotos(serial, remoteDir, localDest string, move bool, progress func(done, total int, name string)) (int, int, int, error) {
	files, err := ListRemoteMedia(serial, remoteDir)
	if err != nil {
		return 0, 0, 0, err
	}
	if len(files) == 0 {
		return 0, 0, 0, nil
	}
	if err := os.MkdirAll(localDest, 0o755); err != nil {
		return 0, 0, 0, err
	}

	pulled, moved, failed := 0, 0, 0
	for i, remotePath := range files {
		if progress != nil {
			progress(i, len(files), filepath.Base(remotePath))
		}
		rel := strings.TrimPrefix(remotePath, remoteDir)
		rel = strings.TrimPrefix(rel, "/")
		localPath := filepath.Join(localDest, rel)
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			failed++
			continue
		}
		if _, err := Run(serial, "pull", remotePath, localPath); err != nil {
			failed++
			continue
		}
		pulled++
		if move {
			if err := RemoteDelete(serial, remotePath); err != nil {
				failed++
			} else {
				moved++
			}
		}
	}
	return pulled, moved, failed, nil
}
