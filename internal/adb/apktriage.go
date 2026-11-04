package adb

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
)

// SecretFinding is a potential secret (API key, token...) found inside
// an APK's code or assets.
type SecretFinding struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

// APKTriage is the result of a static triage of one APK file.
type APKTriage struct {
	Package     string          `json:"package"`
	APKPath     string          `json:"apk_path"`
	SizeMB      float64         `json:"size_mb"`
	Permissions []string        `json:"permissions"`
	Dangerous   []string        `json:"dangerous_permissions"`
	Trackers    []string        `json:"trackers"`
	URLs        []string        `json:"urls"`
	Secrets     []SecretFinding `json:"secrets"`
	NativeLibs  []string        `json:"native_libs"`
	DEXFiles    int             `json:"dex_files"`
}

var urlRe = regexp.MustCompile(`https?://[A-Za-z0-9./_%?&=:+#@$!*,;-]{4,120}`)

var dangerousPerms = map[string]bool{}

var knownTrackers = []struct {
	name   string
	marker string
}{}

var secretPatterns = []struct {
	kind    string
	pattern *regexp.Regexp
}{}

func init() {
	for _, p := range []string{
		"CAMERA",
		"RECORD_AUDIO",
		"READ_CONTACTS",
		"WRITE_CONTACTS",
		"READ_SMS",
		"SEND_SMS",
		"RECEIVE_SMS",
		"RECEIVE_MMS",
		"READ_CALL_LOG",
		"WRITE_CALL_LOG",
		"CALL_PHONE",
		"ACCESS_FINE_LOCATION",
		"ACCESS_COARSE_LOCATION",
		"ACCESS_BACKGROUND_LOCATION",
		"READ_EXTERNAL_STORAGE",
		"WRITE_EXTERNAL_STORAGE",
		"MANAGE_EXTERNAL_STORAGE",
		"REQUEST_INSTALL_PACKAGES",
		"SYSTEM_ALERT_WINDOW",
		"READ_PHONE_STATE",
		"READ_PHONE_NUMBERS",
		"BODY_SENSORS",
		"QUERY_ALL_PACKAGES",
		"BIND_ACCESSIBILITY_SERVICE",
		"PACKAGE_USAGE_STATS",
		"BIND_DEVICE_ADMIN",
		"REQUEST_IGNORE_BATTERY_OPTIMIZATIONS",
	} {
		dangerousPerms[p] = true
	}

	for _, e := range []string{
		"Google Ads|com.google.android.gms.ads",
		"Firebase Analytics|com.google.firebase.analytics",
		"Crashlytics|com.crashlytics.",
		"Facebook SDK|com.facebook.",
		"Appsflyer|appsflyer",
		"Adjust|com.adjust.",
		"Amplitude|com.amplitude.",
		"Mixpanel|com.mixpanel.",
		"Segment|com.segment.analytics",
		"Branch|io.branch.",
		"Unity Ads|com.unity3d.ads",
		"AppLovin|com.applovin",
		"AdColony|com.adcolony",
		"Chartboost|com.chartboost",
		"IronSource|com.ironsource.",
		"Kochava|com.kochava.",
		"Singular|com.singular.",
		"Tapjoy|com.tapjoy.",
		"MoPub|com.mopub.",
		"StartApp|com.startapp.",
	} {
		parts := strings.SplitN(e, "|", 2)
		knownTrackers = append(knownTrackers, struct {
			name   string
			marker string
		}{name: parts[0], marker: parts[1]})
	}

	for _, e := range []string{
		"Google API key|AIza[0-9A-Za-z_-]{35}",
		"AWS access key|AKIA[0-9A-Z]{16}",
		"Stripe key|sk_(?:live|test)_[0-9a-zA-Z]{10,}",
		"GitHub token|gh[pousr]_[0-9A-Za-z]{20,}",
		"Slack token|xox[baprs]-[0-9A-Za-z-]{10,}",
		"Telegram bot token|[0-9]{8,10}:AA[0-9A-Za-z_-]{30,}",
		"JWT|eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}",
		"Private key|-----BEGIN [A-Z ]*PRIVATE KEY",
	} {
		parts := strings.SplitN(e, "|", 2)
		secretPatterns = append(secretPatterns, struct {
			kind    string
			pattern *regexp.Regexp
		}{kind: parts[0], pattern: regexp.MustCompile(parts[1])})
	}
}

// TriageAPK runs a static triage of a local APK file: permissions and
// dangerous permissions extracted from the binary manifest's UTF-16
// string pool, tracker SDKs, URLs and potential secrets from dex and
// asset strings, plus a dex/native-lib inventory. Nothing inside the
// APK is ever executed.
func TriageAPK(apkPath, pkg string) (*APKTriage, error) {
	zr, err := zip.OpenReader(apkPath)
	if err != nil {
		return nil, fmt.Errorf("cannot open APK: %w", err)
	}
	defer zr.Close()

	t := &APKTriage{Package: pkg, APKPath: apkPath}
	if fi, err := os.Stat(apkPath); err == nil {
		t.SizeMB = float64(fi.Size()) / (1024 * 1024)
	}

	var manifest []byte
	for _, f := range zr.File {
		switch {
		case f.Name == "AndroidManifest.xml":
			manifest, _ = readZipEntry(f)
		case strings.HasSuffix(f.Name, ".dex"):
			t.DEXFiles++
		case strings.HasPrefix(f.Name, "lib/") && strings.HasSuffix(f.Name, ".so"):
			arch := strings.Split(f.Name, "/")[1]
			if !strContains(t.NativeLibs, arch) {
				t.NativeLibs = append(t.NativeLibs, arch)
			}
		}
	}
	sort.Strings(t.NativeLibs)

	if manifest != nil {
		permSet := map[string]bool{}
		for _, s := range utf16Strings(manifest) {
			if i := strings.Index(s, "permission."); i >= 0 {
				permSet[s[i:]] = true
			}
		}
		for p := range permSet {
			t.Permissions = append(t.Permissions, p)
		}
		sort.Strings(t.Permissions)
		t.Dangerous = dangerousOf(t.Permissions)
	}

	var scan []byte
	for _, f := range zr.File {
		scanable := f.Name == "AndroidManifest.xml" ||
			strings.HasSuffix(f.Name, ".dex") ||
			(strings.HasPrefix(f.Name, "assets/") && f.UncompressedSize64 <= 20*1024*1024)
		if !scanable || f.UncompressedSize64 > 60*1024*1024 {
			continue
		}
		if data, rerr := readZipEntry(f); rerr == nil {
			scan = append(scan, data...)
		}
	}
	if len(scan) > 0 {
		s := string(scan)
		for _, tr := range knownTrackers {
			if strings.Contains(s, tr.marker) {
				t.Trackers = append(t.Trackers, tr.name)
			}
		}
		urlSet := map[string]bool{}
		for _, u := range urlRe.FindAllString(s, 500) {
			u = strings.TrimRight(u, ".,;:)'\">")
			if u == "" || benignURL(u) {
				continue
			}
			urlSet[u] = true
		}
		for u := range urlSet {
			t.URLs = append(t.URLs, u)
		}
		sort.Strings(t.URLs)
		if len(t.URLs) > 40 {
			t.URLs = t.URLs[:40]
		}
		t.Secrets = findSecrets(s)
	}

	return t, nil
}

// Report renders a human-readable triage summary.
func (t *APKTriage) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "🔍 APK Triage — %s (%.1f MB · %d dex · libs: %s)\n", t.Package, t.SizeMB, t.DEXFiles, strings.Join(t.NativeLibs, ", "))
	b.WriteString(strings.Repeat("─", 70))
	b.WriteString("\n")
	fmt.Fprintf(&b, "  Permissions: %d total", len(t.Permissions))
	if len(t.Dangerous) > 0 {
		fmt.Fprintf(&b, " — ⚠ %d dangerous:\n    %s", len(t.Dangerous), strings.Join(t.Dangerous, ", "))
	}
	b.WriteString("\n")
	if len(t.Trackers) > 0 {
		fmt.Fprintf(&b, "  Trackers/SDKs (%d): %s\n", len(t.Trackers), strings.Join(t.Trackers, ", "))
	}
	fmt.Fprintf(&b, "  URLs: %d\n", len(t.URLs))
	for i, u := range t.URLs {
		if i >= 15 {
			fmt.Fprintf(&b, "    … and %d more\n", len(t.URLs)-15)
			break
		}
		fmt.Fprintf(&b, "    • %s\n", u)
	}
	if len(t.Secrets) > 0 {
		fmt.Fprintf(&b, "  ⚠ Potential secrets (%d):\n", len(t.Secrets))
		for _, s := range t.Secrets {
			v := s.Value
			if len(v) > 60 {
				v = v[:25] + "…" + v[len(v)-20:]
			}
			fmt.Fprintf(&b, "    • [%s] %s\n", s.Kind, v)
		}
	}
	return b.String()
}

func benignURL(u string) bool {
	for _, b := range []string{
		"schemas.android.com",
		"www.w3.org",
		"xmlpull.org",
		"xml.org",
		"apache.org/licenses",
		"openssl.com",
		"bouncycastle.org",
		"docs.oracle.com",
		"google.com/2008",
	} {
		if strings.Contains(u, b) {
			return true
		}
	}
	return false
}

func readZipEntry(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

func strContains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// dangerousOf filters a permission list down to known-dangerous ones.
func dangerousOf(perms []string) []string {
	var out []string
	for _, p := range perms {
		name := p
		if i := strings.LastIndex(name, "."); i >= 0 {
			name = name[i+1:]
		}
		if dangerousPerms[name] {
			out = append(out, p)
		}
	}
	return out
}

// utf16Strings extracts printable ASCII strings stored as UTF-16LE, as
// found in binary XML string pools (AndroidManifest.xml).
func utf16Strings(data []byte) []string {
	var out []string
	var cur []byte
	for i := 0; i+1 < len(data); i += 2 {
		c := data[i]
		if c >= 0x20 && c < 0x7f && data[i+1] == 0 {
			cur = append(cur, c)
		} else {
			if len(cur) >= 4 {
				out = append(out, string(cur))
			}
			cur = nil
		}
	}
	if len(cur) >= 4 {
		out = append(out, string(cur))
	}
	return out
}

// findSecrets scans raw bytes for well-known secret formats.
func findSecrets(s string) []SecretFinding {
	var out []SecretFinding
	seen := map[string]bool{}
	for _, p := range secretPatterns {
		for _, v := range p.pattern.FindAllString(s, 5) {
			if !seen[p.kind+v] {
				seen[p.kind+v] = true
				out = append(out, SecretFinding{Kind: p.kind, Value: v})
			}
		}
	}
	return out
}
