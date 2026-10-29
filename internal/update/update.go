// Package update implements a self-updater for adbgo: it checks the latest
// GitHub release and swaps in the new binary for the running platform.
package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const releasesAPI = "https://api.github.com/repos/dev-roba/adbgo/releases/latest"

type release struct {
	TagName string  `json:"tag_name"`
	Assets  []asset `json:"assets"`
}

type asset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// Latest returns the latest published release tag (e.g. "v1.0.0").
func Latest() (string, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(releasesAPI)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub API returned %s", resp.Status)
	}
	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return "", err
	}
	if rel.TagName == "" {
		return "", fmt.Errorf("no releases published yet — build from source or run: go install github.com/dev-roba/adbgo@latest")
	}
	return rel.TagName, nil
}

// IsNewer reports whether latest (e.g. "v1.2.0") is newer than current.
func IsNewer(latest, current string) bool {
	a, b := semver(latest), semver(current)
	for i := 0; i < 3; i++ {
		if a[i] > b[i] {
			return true
		}
		if a[i] < b[i] {
			return false
		}
	}
	return false
}

// semver turns "v1.2.3-rc1" into a comparable [3]int (missing parts = 0).
func semver(v string) [3]int {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v = strings.SplitN(v, "-", 2)[0]
	var out [3]int
	parts := strings.SplitN(v, ".", 3)
	for i := 0; i < len(parts) && i < 3; i++ {
		n, err := strconv.Atoi(parts[i])
		if err != nil {
			break
		}
		out[i] = n
	}
	return out
}

// Install downloads the release asset for this platform and replaces the
// current adbgo binary. The running process keeps the old binary in memory;
// the user restarts to pick up the new version.
func Install(tag string) error {
	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Get(releasesAPI)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API returned %s", resp.Status)
	}
	var rel release
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return err
	}

	want := fmt.Sprintf("adbgo_%s_%s", runtime.GOOS, runtime.GOARCH)
	var url string
	for _, a := range rel.Assets {
		if a.Name == want+".tar.gz" || a.Name == want+".zip" {
			url = a.BrowserDownloadURL
			break
		}
	}
	if url == "" {
		return fmt.Errorf("no release asset found for %s/%s — download it manually from github.com/dev-roba/adbgo/releases", runtime.GOOS, runtime.GOARCH)
	}

	tmp, err := os.MkdirTemp("", "adbgo-update")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	archivePath := filepath.Join(tmp, "bundle")
	if err := download(url, archivePath); err != nil {
		return fmt.Errorf("download failed: %w", err)
	}

	exeName := "adbgo"
	if runtime.GOOS == "windows" {
		exeName = "adbgo.exe"
	}
	newBin := filepath.Join(tmp, exeName)
	if strings.HasSuffix(url, ".zip") {
		err = extractZip(archivePath, exeName, newBin)
	} else {
		err = extractTarGz(archivePath, exeName, newBin)
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(newBin, 0o755); err != nil {
		return err
	}

	// Locate the current binary (resolving symlinks, e.g. ~/.local/bin)
	cur, err := os.Executable()
	if err != nil {
		return err
	}
	if resolved, err := filepath.EvalSymlinks(cur); err == nil {
		cur = resolved
	}

	newPath := cur + ".new"
	if err := copyFileTo(newBin, newPath); err != nil {
		return err
	}
	if err := os.Chmod(newPath, 0o755); err != nil {
		return err
	}

	// Windows cannot replace a running executable, so shuffle via .old
	if runtime.GOOS == "windows" {
		os.Remove(cur + ".old")
		if err := os.Rename(cur, cur+".old"); err != nil {
			return err
		}
	}
	if err := os.Rename(newPath, cur); err != nil {
		return fmt.Errorf("could not replace %s: %w", cur, err)
	}
	return nil
}

func download(url, dst string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = io.Copy(f, resp.Body)
	return err
}

func extractTarGz(src, exeName, dst string) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg && filepath.Base(hdr.Name) == exeName {
			out, err := os.Create(dst)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
	}
	return fmt.Errorf("%s not found in archive", exeName)
}

func extractZip(src, exeName, dst string) error {
	zr, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, f := range zr.File {
		if filepath.Base(f.Name) == exeName {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			defer rc.Close()
			out, err := os.Create(dst)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, rc); err != nil {
				out.Close()
				return err
			}
			return out.Close()
		}
	}
	return fmt.Errorf("%s not found in archive", exeName)
}

func copyFileTo(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}
