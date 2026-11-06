package adb

import (
	"fmt"
	"strconv"
	"strings"
)

// shquote wraps s in single quotes so the phone's shell keeps it as a
// single argument. adb shell re-parses the whole command line on the
// device, so arguments containing spaces must be quoted for the remote
// shell, not just for the local exec. This is why the original
// find-my-phone notification text never reached the phone in one piece.
func shquote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

// FindMyPhone makes a connected phone as loud and visible as possible.
// Belt and braces: every volume stream maxed (including the alarm
// stream), Do-not-disturb lifted best-effort, the screen woken and
// keyguard dismissed, a 10-second clock-app timer armed (the fast
// ringer), a two-minute clock-app alarm as backup, the last media
// session resumed for instant noise, vibration buzzes, and a visible
// notification. Timer and alarm fire on the alarm stream, which rings
// even in silent mode. It returns a summary of what actually worked,
// so the user knows exactly what to expect.
func FindMyPhone(serial string) (string, error) {
	// The phone must actually respond, or everything below would
	// silently do nothing and mislead the user.
	if out, err := Run(serial, "shell", "echo", "adbgo"); err != nil || strings.TrimSpace(out) != "adbgo" {
		return "", fmt.Errorf("device %s is not responding over adb", serial)
	}

	var steps []string

	// 1. Some devices mute alarms and media while Do-not-disturb is on;
	// try to lift it. Best effort.
	_, _ = Run(serial, "shell", "cmd", "notification", "set_dnd", "off")

	// 2. Max the volume streams: 2 ring, 3 music, 4 alarm, 5 notification.
	// Indexes above a stream maximum are clamped by the device, so after
	// the classic 15 we also attempt 30 for devices with finer steps.
	for _, stream := range []string{"2", "3", "4", "5"} {
		ok := false
		for _, idx := range []string{"15", "30"} {
			if _, err := Run(serial, "shell", "media", "volume", "--stream", stream, "--set", idx); err == nil {
				ok = true
			}
		}
		if ok {
			steps = append(steps, "stream "+stream+" volume maxed")
		}
	}

	// 3. Wake the screen and dismiss the keyguard so the phone is lit.
	_, _ = Run(serial, "shell", "input", "keyevent", "KEYCODE_WAKEUP")
	_, _ = Run(serial, "shell", "wm", "dismiss-keyguard")
	steps = append(steps, "screen woken")

	// 4. The fast ringer: a 10-second timer armed through the clock app.
	// It rings on the alarm stream, even in silent mode, within seconds.
	if out, err := Run(serial, "shell", timerCmd(10)); err == nil && !amFailed(out) {
		steps = append(steps, "timer ringing in ~10 seconds (fires even in silent mode)")
	}

	// 5. The backup ringer: a real alarm two minutes out, in case the
	// timer request is refused or the timer gets dismissed by accident.
	if h, m, err := nextAlarmTime(serial); err == nil {
		alarmCmd := "am start -a android.intent.action.SET_ALARM" +
			" --ez android.intent.extra.alarm.SKIP_UI true" +
			" --ez android.intent.extra.alarm.VIBRATE true" +
			" --es android.intent.extra.alarm.MESSAGE " + shquote("adbgo: FIND ME") +
			" --ei android.intent.extra.alarm.HOUR " + strconv.Itoa(h) +
			" --ei android.intent.extra.alarm.MINUTES " + strconv.Itoa(m)
		if out, aerr := Run(serial, "shell", alarmCmd); aerr == nil && !amFailed(out) {
			steps = append(steps, fmt.Sprintf("alarm scheduled for %02d:%02d — it will ring even in silent mode", h, m))
		}
	}

	// 6. Instant noise: resume the most recent media session. Whatever
	// was last playing (music, podcast, video) now blasts at max volume.
	played := false
	if _, err := Run(serial, "shell", "cmd", "media_session", "dispatch", "play"); err == nil {
		played = true
	}
	if _, err := Run(serial, "shell", "input", "keyevent", "KEYCODE_MEDIA_PLAY"); err == nil {
		played = true
	}
	if played {
		steps = append(steps, "last media resumed (loud if something was recently playing)")
	}

	// 7. Best-effort vibration buzzes, for phones face-down or buried
	// under a cushion. Command syntax varies across Android versions,
	// so several spellings are tried.
	buzzed := false
	for _, spec := range []string{"oneshot 3000", "waveform 2000,1000,2000", "predefined heavy_click", "predefined heavy"} {
		out, err := Run(serial, "shell", append([]string{"cmd", "vibrator"}, strings.Fields(spec)...)...)
		if err == nil && !strings.Contains(strings.ToLower(out), "error") {
			buzzed = true
			break
		}
	}
	if buzzed {
		steps = append(steps, "vibration buzz requested")
	}

	// 8. A visible notification so the lit screen says where the phone is.
	postStyled := "cmd notification post -S bigtext -t " + shquote("adbgo — FIND ME") +
		" adbgo-find " + shquote("📞 Your phone is HERE — sent from adbgo")
	postPlain := "cmd notification post -t " + shquote("adbgo — FIND ME") +
		" adbgo-find " + shquote("📞 Your phone is HERE — sent from adbgo")
	posted := false
	if out, err := Run(serial, "shell", postStyled); err == nil && !strings.Contains(strings.ToLower(out), "error") {
		posted = true
	}
	if !posted {
		if out, err := Run(serial, "shell", postPlain); err == nil && !strings.Contains(strings.ToLower(out), "error") {
			posted = true
		}
	}
	if posted {
		steps = append(steps, "notification posted")
	}

	return strings.Join(steps, "; "), nil
}

// timerCmd builds an 'am start' command that arms a clock-app timer,
// without opening any UI, that fires length seconds from now.
func timerCmd(length int) string {
	return "am start -a android.intent.action.SET_TIMER" +
		" --ez android.intent.extra.timer.SKIP_UI true" +
		" --ez android.intent.extra.timer.VIBRATE true" +
		" --es android.intent.extra.timer.MESSAGE " + shquote("adbgo: FIND ME") +
		" --ei android.intent.extra.timer.LENGTH " + strconv.Itoa(length)
}

// nextAlarmTime reads the phone clock and returns a time two minutes
// ahead, so the scheduled alarm fires shortly after the call. Two
// minutes rather than one avoids the race where the requested minute
// has already ticked past and the alarm would wait a full day.
func nextAlarmTime(serial string) (int, int, error) {
	out, err := Run(serial, "shell", "date", "+%H:%M")
	if err != nil {
		return 0, 0, err
	}
	return addMinutes(out, 2)
}

// addMinutes parses an "HH:MM" clock reading and advances it by add
// minutes, wrapping past midnight. Pure, unit-tested.
func addMinutes(hhmm string, add int) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(hhmm), ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("unexpected clock output %q", hhmm)
	}
	h, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, err
	}
	m, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, err
	}
	if h < 0 || h > 23 || m < 0 || m > 59 || add < 0 {
		return 0, 0, fmt.Errorf("invalid clock time %q", hhmm)
	}
	total := (h*60 + m + add) % (24 * 60)
	return total / 60, total % 60, nil
}

// amFailed reports whether an 'am start' invocation failed: several
// Android builds print human-readable errors while still exiting zero.
func amFailed(out string) bool {
	l := strings.ToLower(out)
	return strings.Contains(l, "error") ||
		strings.Contains(l, "securityexception") ||
		strings.Contains(l, "permission denial") ||
		strings.Contains(l, "no activity found") ||
		strings.Contains(l, "not started")
}
