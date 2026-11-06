package adb

import "testing"

func TestAddMinutes(t *testing.T) {
	if h, m, err := addMinutes("09:41", 2); err != nil || h != 9 || m != 43 {
		t.Fatalf("addMinutes(09:41) = %02d:%02d (%v), want 09:43", h, m, err)
	}
	if h, m, err := addMinutes("10:00", 2); err != nil || h != 10 || m != 2 {
		t.Fatalf("addMinutes(10:00) = %02d:%02d (%v), want 10:02", h, m, err)
	}
	if h, m, err := addMinutes("23:59", 2); err != nil || h != 0 || m != 1 {
		t.Fatalf("addMinutes(23:59) = %02d:%02d (%v), want 00:01", h, m, err)
	}
	if h, m, err := addMinutes("00:00", 2); err != nil || h != 0 || m != 2 {
		t.Fatalf("addMinutes(00:00) = %02d:%02d (%v), want 00:02", h, m, err)
	}
	if h, m, err := addMinutes(" 18:07\n", 2); err != nil || h != 18 || m != 9 {
		t.Fatalf("addMinutes(18:07 padded) = %02d:%02d (%v), want 18:09", h, m, err)
	}
}

func TestAddMinutesInvalid(t *testing.T) {
	for _, in := range []string{"", "garbage", "9:41:20", "25:00", "09:99", "x9:41", "09:4x"} {
		if _, _, err := addMinutes(in, 2); err == nil {
			t.Fatalf("addMinutes(%q, 2) should fail", in)
		}
	}
}

func TestAMFailed(t *testing.T) {
	failures := []string{
		"Starting: Intent { act=android.intent.action.SET_ALARM }\nError: java.lang.SecurityException: Permission Denial: requires com.android.alarm.permission.SET_ALARM",
		"Error: No activity found to handle Intent { act=android.intent.action.SET_ALARM }",
		"Warning: Activity not started, requires permission",
	}
	for _, f := range failures {
		if !amFailed(f) {
			t.Fatalf("amFailed(%q) = false, want true", f)
		}
	}
	successes := []string{
		"Starting: Intent { act=android.intent.action.SET_ALARM flg=0x10000000 }",
		"",
	}
	for _, s := range successes {
		if amFailed(s) {
			t.Fatalf("amFailed(%q) = true, want false", s)
		}
	}
}

func TestShquote(t *testing.T) {
	if got := shquote("adbgo — FIND ME"); got != "'adbgo — FIND ME'" {
		t.Fatalf("shquote(simple) = %q", got)
	}
	if got := shquote("it's here"); got != `'it'"'"'s here'` {
		t.Fatalf("shquote(embedded quote) = %q", got)
	}
}
