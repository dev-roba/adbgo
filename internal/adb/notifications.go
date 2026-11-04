package adb

import (
	"fmt"
	"regexp"
	"strings"
)

// Notification is one parsed notification record from the device.
type Notification struct {
	Package string `json:"package"`
	Title   string `json:"title"`
	Text    string `json:"text"`
}

var (
	notifPkgRe   = regexp.MustCompile(`pkg=([0-9a-zA-Z._]+)`)
	notifTitleRe = regexp.MustCompile(`android\.title=([^,\]}]+)`)
	notifTextRe  = regexp.MustCompile(`android\.text=([^,\]}]+)`)
)

// ListNotifications parses `dumpsys notification` for the currently active
// notification records. The dump format varies between Android versions,
// so parsing is tolerant: package, title and text are extracted whenever
// they are present. Title/text may be missing on some ROMs.
func ListNotifications(serial string) ([]Notification, error) {
	out, err := Run(serial, "shell", "dumpsys", "notification", "--noredact")
	if err != nil {
		out, err = Run(serial, "shell", "dumpsys", "notification")
		if err != nil {
			return nil, fmt.Errorf("cannot read notifications: %w", err)
		}
	}

	var notifs []Notification
	seen := map[string]bool{}
	for _, chunk := range strings.Split(out, "NotificationRecord{")[1:] {
		pkg := notifPkgRe.FindStringSubmatch(chunk)
		if pkg == nil {
			continue
		}
		title := ""
		if m := notifTitleRe.FindStringSubmatch(chunk); m != nil {
			title = strings.TrimSpace(m[1])
		}
		text := ""
		if m := notifTextRe.FindStringSubmatch(chunk); m != nil {
			text = strings.TrimSpace(m[1])
		}
		key := pkg[1] + "|" + title + "|" + text
		if seen[key] {
			continue
		}
		seen[key] = true
		notifs = append(notifs, Notification{Package: pkg[1], Title: title, Text: text})
	}
	if len(notifs) == 0 {
		return nil, fmt.Errorf("no notification records found — either none are active or this Android version's dumpsys format is not supported")
	}
	return notifs, nil
}
