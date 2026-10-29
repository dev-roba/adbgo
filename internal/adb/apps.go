package adb

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

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

// AllPackages returns all installed package names.
func AllPackages(serial string) ([]string, error) {
	out, err := Run(serial, "shell", "pm", "list", "packages")
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

// InstallPackage handles single APKs as well as Split APK bundles (.xapk, .apks, .zip).
// For bundles, it extracts the archive and runs `adb install-multiple`.
func InstallPackage(serial, pkgPath string, allowDowngrade bool) error {
	lower := strings.ToLower(pkgPath)
	if strings.HasSuffix(lower, ".xapk") || strings.HasSuffix(lower, ".apks") || strings.HasSuffix(lower, ".zip") {
		return installSplitBundle(serial, pkgPath, allowDowngrade)
	}
	return InstallAPK(serial, pkgPath, allowDowngrade)
}

// InstallAPK installs a standard single APK with streamed output.
func InstallAPK(serial, apkPath string, allowDowngrade bool) error {
	args := []string{"install", "-r"}
	if allowDowngrade {
		args = append(args, "-d")
	}
	args = append(args, apkPath)
	return RunStreaming(serial, args...)
}

func installSplitBundle(serial, bundlePath string, allowDowngrade bool) error {
	tmpDir, err := os.MkdirTemp("", "adbgo-split-apk-*")
	if err != nil {
		return fmt.Errorf("failed to create temp dir: %w", err)
	}
	defer os.RemoveAll(tmpDir)

	r, err := zip.OpenReader(bundlePath)
	if err != nil {
		return fmt.Errorf("failed to open bundle as zip: %w", err)
	}
	defer r.Close()

	var apkFiles []string
	for _, f := range r.File {
		if strings.HasSuffix(strings.ToLower(f.Name), ".apk") {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			destPath := filepath.Join(tmpDir, filepath.Base(f.Name))
			outFile, err := os.OpenFile(destPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
			if err != nil {
				rc.Close()
				return err
			}
			_, err = io.Copy(outFile, rc)
			outFile.Close()
			rc.Close()
			if err != nil {
				return err
			}
			apkFiles = append(apkFiles, destPath)
		}
	}

	if len(apkFiles) == 0 {
		return fmt.Errorf("no APK files found inside bundle %s", bundlePath)
	}

	args := []string{"install-multiple", "-r"}
	if allowDowngrade {
		args = append(args, "-d")
	}
	args = append(args, apkFiles...)
	fmt.Printf("Installing %d split APK(s)...\n", len(apkFiles))
	return RunStreaming(serial, args...)
}

// ExtractAPK extracts all APK files of an installed app to a local directory.
func ExtractAPK(serial, packageName, destDir string) ([]string, error) {
	if destDir == "" {
		destDir = "."
	}
	out, err := Run(serial, "shell", "pm", "path", packageName)
	if err != nil {
		return nil, fmt.Errorf("failed to find path for package %s: %w", packageName, err)
	}

	var extracted []string
	index := 0
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "package:") {
			continue
		}
		remotePath := strings.TrimPrefix(line, "package:")
		var localName string
		if index == 0 {
			localName = fmt.Sprintf("%s_base.apk", packageName)
		} else {
			localName = fmt.Sprintf("%s_split_%d.apk", packageName, index)
		}
		dest := filepath.Join(destDir, localName)
		if _, err := Run(serial, "pull", remotePath, dest); err != nil {
			return extracted, fmt.Errorf("failed to pull %s: %w", remotePath, err)
		}
		extracted = append(extracted, dest)
		index++
	}

	if len(extracted) == 0 {
		return nil, fmt.Errorf("no APK paths found for %s", packageName)
	}
	return extracted, nil
}

// LaunchApp starts the default launcher activity for a package.
func LaunchApp(serial, packageName string) error {
	_, err := Run(serial, "shell", "monkey", "-p", packageName, "-c", "android.intent.category.LAUNCHER", "1")
	return err
}

// ClearAppData clears all user data for an app.
func ClearAppData(serial, packageName string) error {
	_, err := Run(serial, "shell", "pm", "clear", packageName)
	return err
}

// ForceStopApp force-stops an application.
func ForceStopApp(serial, packageName string) error {
	_, err := Run(serial, "shell", "am", "force-stop", packageName)
	return err
}

// UninstallApp uninstalls a package.
func UninstallApp(serial, packageName string) error {
	_, err := Run(serial, "uninstall", packageName)
	return err
}
