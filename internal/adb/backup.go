package adb

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// DefaultBackupRoot is the folder adbgo saves phone backups into: one
// subfolder per serial, one subfolder per run, newest last.
const DefaultBackupRoot = "adbgo-backups"

// BackupOptions selects which categories to back up or restore.
type BackupOptions struct {
	Contacts bool
	SMS      bool
	CallLog  bool
	Settings bool
	Apps     bool
	Media    bool
}

// Any reports whether at least one category is selected.
func (o BackupOptions) Any() bool {
	return o.Contacts || o.SMS || o.CallLog || o.Settings || o.Apps || o.Media
}

// AllBackupOptions selects every category; media controls whether the
// heavy DCIM/Pictures pull is included.
func AllBackupOptions(media bool) BackupOptions {
	return BackupOptions{Contacts: true, SMS: true, CallLog: true, Settings: true, Apps: true, Media: media}
}

// ParseBackupChoices maps category words onto options. "everything"
// selects all categories, including photos and videos.
func ParseBackupChoices(words []string) (BackupOptions, error) {
	opts := BackupOptions{}
	none := true
	for _, w := range words {
		switch strings.ToLower(strings.TrimSpace(w)) {
		case "everything", "all":
			return AllBackupOptions(true), nil
		case "contacts", "numbers", "phonebook":
			opts.Contacts = true
		case "sms", "texts", "messages":
			opts.SMS = true
		case "calls", "calllog", "call_log":
			opts.CallLog = true
		case "settings":
			opts.Settings = true
		case "apps", "applist":
			opts.Apps = true
		case "media", "photos":
			opts.Media = true
		default:
			return opts, fmt.Errorf("unknown category %q — valid: contacts, sms, calls, settings, apps, media, everything", w)
		}
		none = false
	}
	if none {
		return opts, fmt.Errorf("no categories given — valid: contacts, sms, calls, settings, apps, media, everything")
	}
	return opts, nil
}

// Contact is one address book entry: a display name plus its phone
// numbers and email addresses.
type Contact struct {
	Name   string   `json:"name"`
	Phones []string `json:"phones,omitempty"`
	Emails []string `json:"emails,omitempty"`
}

// Message is one backed-up SMS.
type Message struct {
	Address string `json:"address"`
	Date    int64  `json:"date"`
	Type    int    `json:"type"`
	Body    string `json:"body"`
}

// Call is one call log entry.
type Call struct {
	Number   string `json:"number"`
	Date     int64  `json:"date"`
	Type     int    `json:"type"`
	Duration int64  `json:"duration"`
}

// SettingsSet maps a settings namespace to its key/value pairs.
type SettingsSet map[string]map[string]string

// BackupMeta records what a backup folder holds.
type BackupMeta struct {
	Serial   string    `json:"serial"`
	Created  time.Time `json:"created"`
	Contacts int       `json:"contacts"`
	SMS      int       `json:"sms"`
	Calls    int       `json:"calls"`
	Settings int       `json:"settings"`
	Apps     int       `json:"apps"`
	Media    []string  `json:"media,omitempty"`
}

// BackupResult summarizes what a backup or restore did.
type BackupResult struct {
	Dir         string
	Contacts    int
	SMS         int
	Calls       int
	Settings    int
	Apps        int
	Media       []string
	MissingApps []string
	Restored    int
	Skipped     int
	Failed      int
	Warnings    []string
}

// Warn records a best-effort warning on the result.
func (r *BackupResult) Warn(msg string) {
	r.Warnings = append(r.Warnings, msg)
}

// Summary renders what a backup saved.
func (r *BackupResult) Summary() string {
	var parts []string
	if r.Contacts > 0 {
		parts = append(parts, strconv.Itoa(r.Contacts)+" contacts")
	}
	if r.SMS > 0 {
		parts = append(parts, strconv.Itoa(r.SMS)+" SMS")
	}
	if r.Calls > 0 {
		parts = append(parts, strconv.Itoa(r.Calls)+" calls")
	}
	if r.Settings > 0 {
		parts = append(parts, strconv.Itoa(r.Settings)+" settings")
	}
	if r.Apps > 0 {
		parts = append(parts, strconv.Itoa(r.Apps)+" apps")
	}
	if len(r.Media) > 0 {
		parts = append(parts, strings.Join(r.Media, " + "))
	}
	if len(parts) == 0 {
		return "nothing found to save"
	}
	return strings.Join(parts, ", ")
}

// RestoreSummary renders what a restore put back on the phone.
func (r *BackupResult) RestoreSummary() string {
	var parts []string
	if r.Contacts > 0 {
		parts = append(parts, strconv.Itoa(r.Contacts)+" contacts")
	}
	if r.SMS > 0 {
		parts = append(parts, strconv.Itoa(r.SMS)+" SMS")
	}
	if r.Calls > 0 {
		parts = append(parts, strconv.Itoa(r.Calls)+" calls")
	}
	if r.Settings > 0 {
		parts = append(parts, strconv.Itoa(r.Settings)+" settings")
	}
	if len(r.Media) > 0 {
		parts = append(parts, strings.Join(r.Media, " + ")+" pushed back")
	}
	if len(parts) == 0 {
		parts = append(parts, "nothing new")
	}
	s := strings.Join(parts, ", ") + " restored"
	if r.Skipped > 0 {
		s += ", " + strconv.Itoa(r.Skipped) + " skipped (already on the phone)"
	}
	if r.Failed > 0 {
		s += ", " + strconv.Itoa(r.Failed) + " failed"
	}
	return s
}

var contactKeys = []string{"raw_contact_id", "mimetype", "display_name", "data1"}
var smsKeys = []string{"address", "date", "type", "body"}
var callKeys = []string{"number", "date", "type", "duration"}

// parseContentRows splits raw `content query` output into rows. Values
// can embed real newlines (SMS bodies), so any line that does not start
// a new "Row: " is glued back onto the previous row.
func parseContentRows(out string) []string {
	var rows []string
	lines := strings.Split(out, "\n")
	for i, line := range lines {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, "Row: ") {
			rows = append(rows, line)
			continue
		}
		if len(rows) == 0 {
			continue
		}
		if i == len(lines)-1 && strings.TrimSpace(line) == "" {
			continue // artifact of the trailing newline, not a real line
		}
		rows[len(rows)-1] += "\n" + line
	}
	return rows
}

// parseContentRow splits one `content query` row into key/value pairs.
// Only tokens that open one of the known keys start a new pair; every
// other token is glued back into the previous value, so "Wachira, Robert"
// survives as one display name and message bodies keep their commas.
func parseContentRow(row string, keys []string) map[string]string {
	s := row
	if strings.HasPrefix(s, "Row: ") {
		rest := s[len("Row: "):]
		if i := strings.IndexByte(rest, ' '); i >= 0 {
			s = rest[i+1:]
		}
	}
	out := map[string]string{}
	var curKey, curVal string
	flush := func() {
		if curKey != "" {
			out[curKey] = curVal
		}
	}
	for _, tok := range strings.Split(s, ", ") {
		matched := ""
		for _, k := range keys {
			if strings.HasPrefix(tok, k+"=") {
				matched = k
				break
			}
		}
		if matched != "" {
			flush()
			curKey = matched
			curVal = tok[len(matched)+1:]
		} else if curKey != "" {
			curVal += ", " + tok
		}
	}
	flush()
	return out
}

// sliceHas reports whether ss contains s.
func sliceHas(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

// normPhone keeps only digits so phone numbers match no matter how the
// device formatted them (+, spaces, dashes).
func normPhone(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ParseContacts turns a raw contacts dump into grouped Contact records:
// one record per raw contact with its display name, phones and emails.
func ParseContacts(dump string) []Contact {
	var order []int
	byID := map[int]*Contact{}
	for _, row := range parseContentRows(dump) {
		kv := parseContentRow(row, contactKeys)
		id, err := strconv.Atoi(strings.TrimSpace(kv["raw_contact_id"]))
		if err != nil {
			continue
		}
		c, ok := byID[id]
		if !ok {
			c = &Contact{}
			byID[id] = c
			order = append(order, id)
		}
		switch kv["mimetype"] {
		case "vnd.android.cursor.item/name":
			if c.Name == "" {
				c.Name = strings.TrimSpace(kv["display_name"])
			}
		case "vnd.android.cursor.item/phone_v2":
			if p := strings.TrimSpace(kv["data1"]); p != "" && !sliceHas(c.Phones, p) {
				c.Phones = append(c.Phones, p)
			}
		case "vnd.android.cursor.item/email_v2":
			if e := strings.TrimSpace(kv["data1"]); e != "" && !sliceHas(c.Emails, e) {
				c.Emails = append(c.Emails, e)
			}
		}
	}
	out := make([]Contact, 0, len(order))
	for _, id := range order {
		c := byID[id]
		if c.Name != "" || len(c.Phones) > 0 || len(c.Emails) > 0 {
			out = append(out, *c)
		}
	}
	return out
}

// ParseSMS turns a raw content://sms dump into Message records.
func ParseSMS(dump string) []Message {
	var out []Message
	for _, row := range parseContentRows(dump) {
		kv := parseContentRow(row, smsKeys)
		m := Message{
			Address: strings.TrimSpace(kv["address"]),
			Body:    kv["body"],
		}
		m.Date, _ = strconv.ParseInt(strings.TrimSpace(kv["date"]), 10, 64)
		m.Type, _ = strconv.Atoi(strings.TrimSpace(kv["type"]))
		if m.Address == "" && m.Body == "" {
			continue
		}
		if m.Type == 0 {
			m.Type = 1
		}
		out = append(out, m)
	}
	return out
}

// ParseCallLog turns a raw content://call_log dump into Call records.
func ParseCallLog(dump string) []Call {
	var out []Call
	for _, row := range parseContentRows(dump) {
		kv := parseContentRow(row, callKeys)
		c := Call{Number: strings.TrimSpace(kv["number"])}
		if c.Number == "" {
			continue
		}
		c.Date, _ = strconv.ParseInt(strings.TrimSpace(kv["date"]), 10, 64)
		c.Type, _ = strconv.Atoi(strings.TrimSpace(kv["type"]))
		c.Duration, _ = strconv.ParseInt(strings.TrimSpace(kv["duration"]), 10, 64)
		out = append(out, c)
	}
	return out
}

// ParseSettingsList turns "settings list" output into key/value pairs.
// Lines without '=' are glued back onto the previous value.
func ParseSettingsList(out string) map[string]string {
	m := map[string]string{}
	var lastKey string
	for _, line := range strings.Split(out, "\n") {
		if i := strings.IndexByte(line, '='); i > 0 {
			lastKey = line[:i]
			m[lastKey] = line[i+1:]
		} else if lastKey != "" {
			m[lastKey] += "\n" + line
		}
	}
	return m
}

// ParsePackages extracts package names from "pm list packages" output or
// from a saved apps.txt (bare names, one per line).
func ParsePackages(out string) []string {
	var pkgs []string
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 1 {
			continue
		}
		if p := strings.TrimPrefix(fields[0], "package:"); p != "" {
			pkgs = append(pkgs, p)
		}
	}
	return pkgs
}

// writeJSON marshals v into dir/name, indented for humans.
func writeJSON(dir, name string, v any) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), data, 0o644)
}

// readJSON loads dir/name into v.
func readJSON(dir, name string, v any) error {
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// fileExists reports whether path is present.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// smsKey identifies one message across backup and phone.
func smsKey(m Message) string {
	return m.Address + "|" + strconv.FormatInt(m.Date, 10) + "|" + m.Body
}

// callKey identifies one call log entry across backup and phone.
func callKey(c Call) string {
	return c.Number + "|" + strconv.FormatInt(c.Date, 10)
}

// contactOnPhone reports whether the phone already has this contact: any
// matching phone number, or the same name for a contact with no numbers.
func contactOnPhone(have map[string]bool, c Contact) bool {
	for _, p := range c.Phones {
		if have[normPhone(p)] {
			return true
		}
	}
	if len(c.Phones) == 0 && c.Name != "" {
		return have["name:"+strings.ToLower(c.Name)]
	}
	return false
}

// bindContactData attaches one data row (name, phone or email) to a raw
// contact. Phone rows also carry a type column.
func bindContactData(serial string, rawID int, mime, value string, phone bool) error {
	args := []string{"shell", "content", "insert",
		"--uri", "content://com.android.contacts/data",
		"--bind", "raw_contact_id:i:" + strconv.Itoa(rawID),
		"--bind", "mimetype:s:" + mime,
		"--bind", "data1:s:" + shquote(value)}
	if phone {
		args = append(args, "--bind", "data2:i:2")
	}
	_, err := Run(serial, args...)
	return err
}

// insertContact writes one contact back through the contacts provider: a
// raw contact is created under a dedicated adbgo account, its fresh id is
// read back, then name/phone/email data rows are attached to it.
func insertContact(serial string, c Contact) error {
	if _, err := Run(serial, "shell", "content", "insert",
		"--uri", "content://com.android.contacts/raw_contacts",
		"--bind", "account_name:s:adbgo",
		"--bind", "account_type:s:com.adbgo.backup"); err != nil {
		return err
	}
	out, err := Run(serial, "shell", "content", "query",
		"--uri", "content://com.android.contacts/raw_contacts",
		"--projection", "_id",
		"--where", "account_type='com.adbgo.backup'",
		"--sort", shquote("_id DESC"))
	if err != nil {
		return err
	}
	rows := parseContentRows(out)
	if len(rows) == 0 {
		return fmt.Errorf("could not read back the new contact id")
	}
	kv := parseContentRow(rows[0], []string{"_id"})
	id, err := strconv.Atoi(strings.TrimSpace(kv["_id"]))
	if err != nil {
		return fmt.Errorf("unexpected raw contact id %q", kv["_id"])
	}
	if c.Name != "" {
		if err := bindContactData(serial, id, "vnd.android.cursor.item/name", c.Name, false); err != nil {
			return err
		}
	}
	for _, p := range c.Phones {
		if err := bindContactData(serial, id, "vnd.android.cursor.item/phone_v2", p, true); err != nil {
			return err
		}
	}
	for _, e := range c.Emails {
		if err := bindContactData(serial, id, "vnd.android.cursor.item/email_v2", e, false); err != nil {
			return err
		}
	}
	return nil
}

// Backup copies the selected categories off the phone into a timestamped
// folder under backupRoot: <root>/<serial>/<date-time>/...
func Backup(serial, backupRoot string, opts BackupOptions) (*BackupResult, error) {
	if !opts.Any() {
		return nil, fmt.Errorf("nothing selected to back up")
	}
	if out, err := Run(serial, "shell", "echo", "adbgo"); err != nil || strings.TrimSpace(out) != "adbgo" {
		return nil, fmt.Errorf("device %s is not responding over adb", serial)
	}
	res := &BackupResult{}
	dir := filepath.Join(backupRoot, sanitize(serial), time.Now().Format("20060102-150405"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	res.Dir = dir

	if opts.Contacts {
		dump, err := Run(serial, "shell", "content", "query",
			"--uri", "content://com.android.contacts/data",
			"--projection", "raw_contact_id:mimetype:display_name:data1")
		if err != nil {
			res.Warn("contacts query failed: " + err.Error())
		} else {
			contacts := ParseContacts(dump)
			if err := writeJSON(dir, "contacts.json", contacts); err != nil {
				res.Warn("contacts save failed: " + err.Error())
			} else {
				res.Contacts = len(contacts)
				if res.Contacts == 0 {
					res.Warn("no contacts found on the phone")
				}
			}
		}
	}

	if opts.SMS {
		dump, err := Run(serial, "shell", "content", "query",
			"--uri", "content://sms",
			"--projection", "address:date:type:body")
		if err != nil {
			res.Warn("SMS query failed: " + err.Error())
		} else {
			msgs := ParseSMS(dump)
			if err := writeJSON(dir, "sms.json", msgs); err != nil {
				res.Warn("SMS save failed: " + err.Error())
			} else {
				res.SMS = len(msgs)
				if res.SMS == 0 {
					res.Warn("no SMS found on the phone")
				}
			}
		}
	}

	if opts.CallLog {
		dump, err := Run(serial, "shell", "content", "query",
			"--uri", "content://call_log/calls",
			"--projection", "number:date:type:duration")
		if err != nil {
			res.Warn("call log query failed: " + err.Error())
		} else {
			calls := ParseCallLog(dump)
			if err := writeJSON(dir, "calls.json", calls); err != nil {
				res.Warn("call log save failed: " + err.Error())
			} else {
				res.Calls = len(calls)
				if res.Calls == 0 {
					res.Warn("no call log found on the phone")
				}
			}
		}
	}

	if opts.Settings {
		set := SettingsSet{}
		for _, ns := range []string{"global", "secure", "system"} {
			out, err := Run(serial, "shell", "settings", "list", ns)
			if err != nil {
				res.Warn(ns + " settings query failed: " + err.Error())
				continue
			}
			set[ns] = ParseSettingsList(out)
			res.Settings += len(set[ns])
		}
		if err := writeJSON(dir, "settings.json", set); err != nil {
			res.Warn("settings save failed: " + err.Error())
		}
	}

	if opts.Apps {
		out, err := Run(serial, "shell", "pm", "list", "packages", "-3")
		if err != nil {
			res.Warn("app list query failed: " + err.Error())
		} else {
			pkgs := ParsePackages(out)
			sort.Strings(pkgs)
			data := strings.Join(pkgs, "\n") + "\n"
			if err := os.WriteFile(filepath.Join(dir, "apps.txt"), []byte(data), 0o644); err != nil {
				res.Warn("app list save failed: " + err.Error())
			} else {
				res.Apps = len(pkgs)
			}
		}
	}

	if opts.Media {
		dst := filepath.Join(dir, "media")
		if err := os.MkdirAll(dst, 0o755); err != nil {
			res.Warn("media folder failed: " + err.Error())
		} else {
			for _, remote := range []string{"DCIM", "Pictures"} {
				if _, err := Run(serial, "pull", "/sdcard/"+remote, dst); err != nil {
					res.Warn(remote + " pull failed (absent or unreadable?) — " + err.Error())
					continue
				}
				res.Media = append(res.Media, remote)
			}
		}
	}

	meta := BackupMeta{
		Serial:   serial,
		Created:  time.Now(),
		Contacts: res.Contacts,
		SMS:      res.SMS,
		Calls:    res.Calls,
		Settings: res.Settings,
		Apps:     res.Apps,
		Media:    res.Media,
	}
	if err := writeJSON(dir, "meta.json", meta); err != nil {
		res.Warn("meta save failed: " + err.Error())
	}
	return res, nil
}

// Restore pushes the selected categories from a backup folder back to
// the phone. Items the phone already has are skipped, so restoring never
// duplicates data — "undo a deletion" and "full recovery after a factory
// reset" are the same operation. When nothing is selected, everything
// the backup holds is restored.
func Restore(serial, dir string, opts BackupOptions) (*BackupResult, error) {
	if out, err := Run(serial, "shell", "echo", "adbgo"); err != nil || strings.TrimSpace(out) != "adbgo" {
		return nil, fmt.Errorf("device %s is not responding over adb", serial)
	}
	if !opts.Any() {
		opts = CategoriesIn(dir)
		if !opts.Any() {
			return nil, fmt.Errorf("backup folder has no restorable data")
		}
	}
	res := &BackupResult{Dir: dir}

	if opts.Contacts {
		var contacts []Contact
		if err := readJSON(dir, "contacts.json", &contacts); err != nil {
			res.Warn("contacts: " + err.Error())
		} else {
			have := map[string]bool{}
			if dump, err := Run(serial, "shell", "content", "query",
				"--uri", "content://com.android.contacts/data",
				"--projection", "raw_contact_id:mimetype:display_name:data1"); err == nil {
				for _, c := range ParseContacts(dump) {
					for _, p := range c.Phones {
						have[normPhone(p)] = true
					}
					if c.Name != "" {
						have["name:"+strings.ToLower(c.Name)] = true
					}
				}
			}
			for _, c := range contacts {
				if contactOnPhone(have, c) {
					res.Skipped++
					continue
				}
				if err := insertContact(serial, c); err != nil {
					res.Failed++
					continue
				}
				res.Contacts++
			}
		}
	}

	if opts.SMS {
		var msgs []Message
		if err := readJSON(dir, "sms.json", &msgs); err != nil {
			res.Warn("SMS: " + err.Error())
		} else {
			have := map[string]bool{}
			if dump, err := Run(serial, "shell", "content", "query",
				"--uri", "content://sms",
				"--projection", "address:date:type:body"); err == nil {
				for _, m := range ParseSMS(dump) {
					have[smsKey(m)] = true
				}
			}
			for _, m := range msgs {
				if have[smsKey(m)] {
					res.Skipped++
					continue
				}
				args := []string{"shell", "content", "insert",
					"--uri", "content://sms",
					"--bind", "address:s:" + shquote(m.Address),
					"--bind", "type:i:" + strconv.Itoa(m.Type),
					"--bind", "date:l:" + strconv.FormatInt(m.Date, 10),
					"--bind", "body:s:" + shquote(m.Body)}
				if _, err := Run(serial, args...); err != nil {
						res.Failed++
						continue
				}
				res.SMS++
			}
		}
	}

	if opts.CallLog {
		var calls []Call
		if err := readJSON(dir, "calls.json", &calls); err != nil {
			res.Warn("call log: " + err.Error())
		} else {
			have := map[string]bool{}
			if dump, err := Run(serial, "shell", "content", "query",
				"--uri", "content://call_log/calls",
				"--projection", "number:date:type:duration"); err == nil {
				for _, c := range ParseCallLog(dump) {
					have[callKey(c)] = true
				}
			}
			for _, c := range calls {
				if have[callKey(c)] {
					res.Skipped++
					continue
				}
				args := []string{"shell", "content", "insert",
					"--uri", "content://call_log/calls",
					"--bind", "number:s:" + shquote(c.Number),
					"--bind", "type:i:" + strconv.Itoa(c.Type),
					"--bind", "date:l:" + strconv.FormatInt(c.Date, 10),
					"--bind", "duration:l:" + strconv.FormatInt(c.Duration, 10)}
				if _, err := Run(serial, args...); err != nil {
						res.Failed++
						continue
				}
				res.Calls++
			}
		}
	}

	if opts.Settings {
		var set SettingsSet
		if err := readJSON(dir, "settings.json", &set); err != nil {
			res.Warn("settings: " + err.Error())
		} else {
			names := make([]string, 0, len(set))
			for ns := range set {
				names = append(names, ns)
			}
			sort.Strings(names)
			for _, ns := range names {
				keys := make([]string, 0, len(set[ns]))
				for k := range set[ns] {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					_, err := Run(serial, "shell", "settings", "put", ns, shquote(k), shquote(set[ns][k]))
					if err != nil {
						res.Failed++
						continue
					}
					res.Settings++
				}
			}
		}
	}

	if opts.Apps {
		data, err := os.ReadFile(filepath.Join(dir, "apps.txt"))
		if err != nil {
			res.Warn("app list: " + err.Error())
		} else {
			current := map[string]bool{}
			if out, err := Run(serial, "shell", "pm", "list", "packages", "-3"); err == nil {
				for _, p := range ParsePackages(out) {
					current[p] = true
				}
			}
			for _, p := range ParsePackages(string(data)) {
				if !current[p] {
					res.MissingApps = append(res.MissingApps, p)
				}
			}
			res.Apps = len(res.MissingApps)
		}
	}

	if opts.Media {
		entries, err := os.ReadDir(filepath.Join(dir, "media"))
		if err != nil {
			res.Warn("media: " + err.Error())
		} else {
			for _, e := range entries {
				if !e.IsDir() {
					continue
				}
				if _, err := Run(serial, "push", filepath.Join(dir, "media", e.Name()), "/sdcard/"); err != nil {
					res.Warn(e.Name() + " push failed: " + err.Error())
					continue
				}
				res.Media = append(res.Media, e.Name())
			}
		}
	}

	res.Restored = res.Contacts + res.SMS + res.Calls + res.Settings
	return res, nil
}

// CategoriesIn reports which categories a backup folder holds.
func CategoriesIn(dir string) BackupOptions {
	opts := BackupOptions{}
	if fileExists(filepath.Join(dir, "contacts.json")) {
		opts.Contacts = true
	}
	if fileExists(filepath.Join(dir, "sms.json")) {
		opts.SMS = true
	}
	if fileExists(filepath.Join(dir, "calls.json")) {
		opts.CallLog = true
	}
	if fileExists(filepath.Join(dir, "settings.json")) {
		opts.Settings = true
	}
	if fileExists(filepath.Join(dir, "apps.txt")) {
		opts.Apps = true
	}
	if entries, err := os.ReadDir(filepath.Join(dir, "media")); err == nil && len(entries) > 0 {
		opts.Media = true
	}
	return opts
}

// DescribeBackup renders a one-line inventory of a backup folder, used
// in menus and listings.
func DescribeBackup(dir string) string {
	var meta BackupMeta
	if err := readJSON(dir, "meta.json", &meta); err != nil {
		if CategoriesIn(dir).Any() {
			return "(backup without meta)"
		}
		return "(empty)"
	}
	var parts []string
	if meta.Contacts > 0 {
		parts = append(parts, strconv.Itoa(meta.Contacts)+" contacts")
	}
	if meta.SMS > 0 {
		parts = append(parts, strconv.Itoa(meta.SMS)+" SMS")
	}
	if meta.Calls > 0 {
		parts = append(parts, strconv.Itoa(meta.Calls)+" calls")
	}
	if meta.Settings > 0 {
		parts = append(parts, strconv.Itoa(meta.Settings)+" settings")
	}
	if meta.Apps > 0 {
		parts = append(parts, strconv.Itoa(meta.Apps)+" apps")
	}
	if len(meta.Media) > 0 {
		parts = append(parts, strings.Join(meta.Media, " + "))
	}
	when := ""
	if !meta.Created.IsZero() {
		when = meta.Created.Format("2006-01-02 15:04") + " — "
	}
	if len(parts) == 0 {
		return when + "empty backup"
	}
	return when + strings.Join(parts, ", ")
}

// ListBackups returns the backup folders saved for a serial, newest
// first.
func ListBackups(root, serial string) ([]string, error) {
	base := filepath.Join(root, sanitize(serial))
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, filepath.Join(base, n))
	}
	return out, nil
}

// LatestBackup returns the newest backup folder for a serial.
func LatestBackup(root, serial string) (string, error) {
	list, err := ListBackups(root, serial)
	if err != nil {
		return "", err
	}
	if len(list) == 0 {
		return "", fmt.Errorf("no backups for %s yet — run 'adbgo backup' first", serial)
	}
	return list[0], nil
}
