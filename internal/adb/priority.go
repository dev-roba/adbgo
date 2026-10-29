package adb

import (
	"fmt"
	"strings"
)

// NormalizePriority validates and normalizes an Android logcat priority.
func NormalizePriority(priority string) (string, error) {
	priority = strings.ToUpper(strings.TrimSpace(priority))
	if priority == "" {
		priority = "V"
	}
	if !strings.Contains("|V|D|I|W|E|F|", "|"+priority+"|") {
		return "", fmt.Errorf("priority must be V, D, I, W, E, or F")
	}
	return priority, nil
}
