package adb

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestParseContentRows(t *testing.T) {
	out := "Row: 0 a=1\nRow: 1 a=2, body=first\nsecond line\nRow: 2 a=3\n"
	rows := parseContentRows(out)
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows, got %d: %q", len(rows), rows)
	}
	if rows[1] != "Row: 1 a=2, body=first\nsecond line" {
		t.Fatalf("continuation not glued: %q", rows[1])
	}
	if rows[2] != "Row: 2 a=3" {
		t.Fatalf("trailing newline artifact leaked into row: %q", rows[2])
	}
}

func TestParseContentRow(t *testing.T) {
	row := "Row: 0 raw_contact_id=5, mimetype=vnd.android.cursor.item/name, display_name=Wachira, Robert, data1=Wachira, Robert"
	kv := parseContentRow(row, contactKeys)
	if kv["raw_contact_id"] != "5" {
		t.Fatalf("raw_contact_id = %q", kv["raw_contact_id"])
	}
	if kv["display_name"] != "Wachira, Robert" {
		t.Fatalf("comma in name not preserved: %q", kv["display_name"])
	}
	if kv["data1"] != "Wachira, Robert" {
		t.Fatalf("comma in value not preserved: %q", kv["data1"])
	}
}

func TestParseContacts(t *testing.T) {
	dump := "Row: 0 raw_contact_id=1, mimetype=vnd.android.cursor.item/name, display_name=Wachira, Robert, data1=Wachira, Robert\n" +
		"Row: 1 raw_contact_id=1, mimetype=vnd.android.cursor.item/phone_v2, display_name=Wachira, Robert, data1=+254711222333\n" +
		"Row: 2 raw_contact_id=1, mimetype=vnd.android.cursor.item/phone_v2, display_name=Wachira, Robert, data1=+254700111111\n" +
		"Row: 3 raw_contact_id=2, mimetype=vnd.android.cursor.item/name, display_name=Amina, data1=Amina\n" +
		"Row: 4 raw_contact_id=2, mimetype=vnd.android.cursor.item/email_v2, display_name=Amina, data1=amina@example.com\n" +
		"Row: 5 garbage without ids\n"
	contacts := ParseContacts(dump)
	if len(contacts) != 2 {
		t.Fatalf("expected 2 contacts, got %d: %+v", len(contacts), contacts)
	}
	if contacts[0].Name != "Wachira, Robert" {
		t.Fatalf("name = %q", contacts[0].Name)
	}
	if len(contacts[0].Phones) != 2 {
		t.Fatalf("phones = %v", contacts[0].Phones)
	}
	if contacts[1].Name != "Amina" || len(contacts[1].Emails) != 1 || contacts[1].Emails[0] != "amina@example.com" {
		t.Fatalf("second contact wrong: %+v", contacts[1])
	}
}

func TestParseSMS(t *testing.T) {
	dump := "Row: 0 address=Mom, date=1700000000000, type=1, body=Dinner at 6, ok?\n" +
		"Row: 1 address=+254722, date=1700000001000, type=2, body=multi\nline body\n"
	msgs := ParseSMS(dump)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d: %+v", len(msgs), msgs)
	}
	if msgs[0].Body != "Dinner at 6, ok?" {
		t.Fatalf("comma in body not preserved: %q", msgs[0].Body)
	}
	if msgs[1].Body != "multi\nline body" {
		t.Fatalf("newline in body not preserved: %q", msgs[1].Body)
	}
	if msgs[0].Date != 1700000000000 || msgs[0].Type != 1 {
		t.Fatalf("fields wrong: %+v", msgs[0])
	}
}

func TestParseCallLog(t *testing.T) {
	dump := "Row: 0 number=+254711222333, date=1700000000000, type=1, duration=120\n" +
		"Row: 1 number=+254700111111, date=1700000001000, type=3, duration=0"
	calls := ParseCallLog(dump)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d: %+v", len(calls), calls)
	}
	if calls[0].Duration != 120 || calls[1].Type != 3 {
		t.Fatalf("fields wrong: %+v", calls)
	}
}

func TestParseSettingsList(t *testing.T) {
	out := "airplane_mode_on=0\ntheme=dark theme, =fancy\nweird\nnext=1"
	m := ParseSettingsList(out)
	if len(m) != 3 {
		t.Fatalf("expected 3 keys, got %d: %v", len(m), m)
	}
	if m["theme"] != "dark theme, =fancy\nweird" {
		t.Fatalf("continuation not glued: %q", m["theme"])
	}
	if m["next"] != "1" || m["airplane_mode_on"] != "0" {
		t.Fatalf("simple values wrong: %v", m)
	}
}

func TestParsePackages(t *testing.T) {
	out := "package:com.foo\npackage:com.bar\r\n\ncom.baz\n"
	pkgs := ParsePackages(out)
	if len(pkgs) != 3 || pkgs[0] != "com.foo" || pkgs[1] != "com.bar" || pkgs[2] != "com.baz" {
		t.Fatalf("packages wrong: %v", pkgs)
	}
}

func TestNormPhone(t *testing.T) {
	if got := normPhone("+254 711-222 333"); got != "254711222333" {
		t.Fatalf("normPhone = %q", got)
	}
}

func TestParseBackupChoices(t *testing.T) {
	opts, err := ParseBackupChoices([]string{"contacts", "messages", "media"})
	if err != nil {
		t.Fatal(err)
	}
	if !opts.Contacts || !opts.SMS || opts.CallLog || opts.Settings || opts.Apps || !opts.Media {
		t.Fatalf("choices wrong: %+v", opts)
	}
	all, err := ParseBackupChoices([]string{"Everything"})
	if err != nil {
		t.Fatal(err)
	}
	if !all.Any() || !all.Media {
		t.Fatalf("everything should select all: %+v", all)
	}
	if _, err := ParseBackupChoices([]string{"nonsense"}); err == nil {
		t.Fatal("expected error for unknown category")
	}
	if _, err := ParseBackupChoices(nil); err == nil {
		t.Fatal("expected error for empty choices")
	}
}

func TestJSONRoundTrip(t *testing.T) {
	dir := t.TempDir()
	in := []Contact{{Name: "Wachira, Robert", Phones: []string{"+254711222333"}}}
	if err := writeJSON(dir, "contacts.json", in); err != nil {
		t.Fatal(err)
	}
	var out []Contact
	if err := readJSON(dir, "contacts.json", &out); err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Name != "Wachira, Robert" || len(out[0].Phones) != 1 {
		t.Fatalf("round trip mismatch: %+v", out)
	}
}

func TestCategoriesIn(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "contacts.json"), []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "media", "DCIM"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts := CategoriesIn(dir)
	if !opts.Contacts || !opts.Media || opts.SMS || opts.CallLog || opts.Settings || opts.Apps {
		t.Fatalf("unexpected categories: %+v", opts)
	}
}

func TestDescribeBackup(t *testing.T) {
	dir := t.TempDir()
	meta := BackupMeta{
		Serial:   "TESTPHONE",
		Created:  time.Date(2026, 11, 6, 17, 40, 0, 0, time.UTC),
		Contacts: 2,
		SMS:      5,
		Calls:    3,
		Settings: 40,
		Apps:     12,
	}
	if err := writeJSON(dir, "meta.json", meta); err != nil {
		t.Fatal(err)
	}
	got := DescribeBackup(dir)
	want := "2026-11-06 17:40 — 2 contacts, 5 SMS, 3 calls, 40 settings, 12 apps"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestListBackups(t *testing.T) {
	root := t.TempDir()
	serial := "TESTPHONE"
	base := filepath.Join(root, sanitize(serial))
	for _, name := range []string{"20260101-100000", "20260102-090000", "20260102-180000"} {
		if err := os.MkdirAll(filepath.Join(base, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(base, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ListBackups(root, serial)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 backups, got %d", len(got))
	}
	if filepath.Base(got[0]) != "20260102-180000" {
		t.Fatalf("newest first failed: %v", got)
	}
	empty, err := ListBackups(root, "NOSUCH")
	if err != nil || len(empty) != 0 {
		t.Fatalf("unknown serial should give empty list, got %v (%v)", empty, err)
	}
	if _, err := LatestBackup(root, "NOSUCH"); err == nil {
		t.Fatal("expected error for missing backups")
	}
}
