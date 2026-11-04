package adb

import (
	"fmt"
	"time"
)

// FindMyPhone makes a connected phone ring loudly: ring/media/notification
// volumes maxed out, screen woken, keyguard dismissed, and a repeating
// alarm notification posted. Best effort — each step tolerates failure
// since device support varies.
func FindMyPhone(serial string) error {
	for _, stream := range []string{"2", "3", "5"} {
		_, _ = Run(serial, "shell", "media", "volume", "--stream", stream, "--set", "15")
	}

	_, _ = Run(serial, "shell", "input", "keyevent", "KEYCODE_WAKEUP")
	_, _ = Run(serial, "shell", "wm", "dismiss-keyguard")

	posted := false
	for i := 0; i < 3; i++ {
		tag := fmt.Sprintf("adbgo%d", i)
		if _, err := Run(serial, "shell", "cmd", "notification", "post", "-S", "alarm",
			"-t", "adbgo — FIND ME", tag, "🔔 Your phone is HERE (sent from adbgo)"); err == nil {
			posted = true
		} else if _, err2 := Run(serial, "shell", "cmd", "notification", "post",
			"-t", "adbgo — FIND ME", tag, "🔔 Your phone is HERE (sent from adbgo)"); err2 == nil {
			posted = true
		}
		time.Sleep(600 * time.Millisecond)
	}
	if !posted {
		return fmt.Errorf("alarm notification not supported on this device — volumes were maxed and the screen woken, but no alarm sound could be posted")
	}
	return nil
}
