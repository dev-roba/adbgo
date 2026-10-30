package adb

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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

// MediaInfo describes one media file on the device.
type MediaInfo struct {
	Name  string // base name
	Path  string // absolute remote path
	Size  int64  // bytes
	Mtime int64  // unix seconds
}

// ListRemoteMediaInfo recursively lists media files under remoteDir with
// their size and modification time. If the device's find/stat combination
// is unavailable it falls back to a plain path listing (size 0, mtime 0).
func ListRemoteMediaInfo(serial, remoteDir string) ([]MediaInfo, error) {
	cmd := "find " + remoteDir + " -type f -exec stat -c '%Y|%s|%n' {} +"
	out, err := Run(serial, "shell", "sh", "-c", cmd)
	if err == nil {
		var infos []MediaInfo
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			parts := strings.SplitN(line, "|", 3)
			if len(parts) != 3 || !IsMediaFile(parts[2]) {
				continue
			}
			mtime, _ := strconv.ParseInt(strings.TrimSpace(parts[0]), 10, 64)
			size, _ := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
			infos = append(infos, MediaInfo{
				Name:  filepath.Base(parts[2]),
				Path:  parts[2],
				Size:  size,
				Mtime: mtime,
			})
		}
		if len(infos) > 0 {
			return infos, nil
		}
	}
	// Fallback: plain listing without metadata.
	paths, lerr := ListRemoteMedia(serial, remoteDir)
	if lerr != nil {
		return nil, lerr
	}
	infos := make([]MediaInfo, 0, len(paths))
	for _, p := range paths {
		infos = append(infos, MediaInfo{Name: filepath.Base(p), Path: p})
	}
	return infos, nil
}

// RecentMedia returns the limit most recently modified media files found
// under the given directories (newest first). Directories that cannot be
// read are skipped.
func RecentMedia(serial string, dirs []string, limit int) []MediaInfo {
	var all []MediaInfo
	for _, d := range dirs {
		if infos, err := ListRemoteMediaInfo(serial, d); err == nil {
			all = append(all, infos...)
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Mtime > all[j].Mtime })
	if limit > 0 && len(all) > limit {
		all = all[:limit]
	}
	return all
}

// PullFiles transfers a chosen list of media files into localDest,
// preserving the directory structure under remoteRoot. With move=true
// each file is deleted from the device only after its pull succeeded.
func PullFiles(serial string, files []MediaInfo, remoteRoot, localDest string, move bool, progress func(done, total int, name string)) (int, int, int, error) {
	if len(files) == 0 {
		return 0, 0, 0, nil
	}
	if err := os.MkdirAll(localDest, 0o755); err != nil {
		return 0, 0, 0, err
	}
	pulled, moved, failed := 0, 0, 0
	for i, f := range files {
		if progress != nil {
			progress(i, len(files), f.Name)
		}
		rel := strings.TrimPrefix(f.Path, remoteRoot)
		rel = strings.TrimPrefix(rel, "/")
		localPath := filepath.Join(localDest, rel)
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			failed++
			continue
		}
		if _, err := Run(serial, "pull", f.Path, localPath); err != nil {
			failed++
			continue
		}
		pulled++
		if move {
			if err := RemoteDelete(serial, f.Path); err != nil {
				failed++
			} else {
				moved++
			}
		}
	}
	return pulled, moved, failed, nil
}

// HumanSize renders a byte count in a human-friendly form.
func HumanSize(b int64) string {
	switch {
	case b >= 1024*1024*1024:
		return fmt.Sprintf("%.1fGB", float64(b)/(1024*1024*1024))
	case b >= 1024*1024:
		return fmt.Sprintf("%.1fMB", float64(b)/(1024*1024))
	case b >= 1024:
		return fmt.Sprintf("%.1fKB", float64(b)/1024)
	default:
		return fmt.Sprintf("%dB", b)
	}
}
