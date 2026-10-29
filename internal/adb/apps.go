package adb

import (
	"fmt"
	"strings"
)

// FindPackagesByKeyword returns installed third-party packages whose package
// name matches the keyword as a case-insensitive substring, so "spotify"
// matches com.spotify.music.
func FindPackagesByKeyword(serial, keyword string) ([]string, error) {
	out, err := Run(serial, "shell", "pm", "list", "packages", "-3")
	if err != nil {
		return nil, err
	}
	kw := strings.ToLower(strings.TrimSpace(keyword))
	if kw == "" {
		return nil, fmt.Errorf("empty keyword")
	}
	var matches []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "package:") {
			continue
		}
		pkg := strings.TrimPrefix(line, "package:")
		if strings.Contains(strings.ToLower(pkg), kw) {
			matches = append(matches, pkg)
		}
	}
	return matches, nil
}

// LaunchApp starts an app's launcher activity. It resolves the launcher
// activity from the full package name first (am start -n), falling back to
// monkey. Note: pkg must be a full package name, not a keyword.
func LaunchApp(serial, pkg string) error {
	out, err := Run(serial, "shell", "cmd", "package", "resolve-activity",
		"--brief", "-c", "android.intent.category.LAUNCHER", pkg)
	if err == nil {
		var component string
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if strings.Contains(line, "/") {
				component = line
			}
		}
		if component != "" {
			_, err = Run(serial, "shell", "am", "start", "-n", component)
			return err
		}
	}
	_, err = Run(serial, "shell", "monkey", "-p", pkg,
		"-c", "android.intent.category.LAUNCHER", "1")
	return err
}
