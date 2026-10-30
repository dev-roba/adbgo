package adb

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// AppSurface describes the externally reachable attack surface of one app,
// parsed from dumpsys package output.
type AppSurface struct {
	Package              string   `json:"package"`
	ExportedActivities   []string `json:"exported_activities,omitempty"`
	ExportedServices     []string `json:"exported_services,omitempty"`
	ExportedReceivers    []string `json:"exported_receivers,omitempty"`
	ExportedProviders    []string `json:"exported_providers,omitempty"`
	Debuggable           bool     `json:"debuggable"`
	AllowBackup          bool     `json:"allow_backup"`
	DangerousPermissions []string `json:"dangerous_permissions,omitempty"`
}

// dangerousPermSet lists Android dangerous permissions that widen an app's
// attack or privacy surface.
var dangerousPermSet = func() map[string]bool {
	m := map[string]bool{}
	for _, p := range []string{
		"android.permission.CAMERA",
		"android.permission.RECORD_AUDIO",
		"android.permission.ACCESS_FINE_LOCATION",
		"android.permission.ACCESS_COARSE_LOCATION",
		"android.permission.ACCESS_BACKGROUND_LOCATION",
		"android.permission.READ_CONTACTS",
		"android.permission.WRITE_CONTACTS",
		"android.permission.READ_CALENDAR",
		"android.permission.WRITE_CALENDAR",
		"android.permission.READ_CALL_LOG",
		"android.permission.WRITE_CALL_LOG",
		"android.permission.READ_SMS",
		"android.permission.SEND_SMS",
		"android.permission.RECEIVE_SMS",
		"android.permission.RECEIVE_MMS",
		"android.permission.READ_PHONE_STATE",
		"android.permission.READ_PHONE_NUMBERS",
		"android.permission.CALL_PHONE",
		"android.permission.GET_ACCOUNTS",
		"android.permission.READ_EXTERNAL_STORAGE",
		"android.permission.WRITE_EXTERNAL_STORAGE",
		"android.permission.MANAGE_EXTERNAL_STORAGE",
		"android.permission.SYSTEM_ALERT_WINDOW",
		"android.permission.QUERY_ALL_PACKAGES",
		"android.permission.REQUEST_INSTALL_PACKAGES",
		"android.permission.BIND_ACCESSIBILITY_SERVICE",
		"android.permission.PACKAGE_USAGE_STATS",
		"android.permission.BODY_SENSORS",
	} {
		m[p] = true
	}
	return m
}()

var resolverComponentRe = regexp.MustCompile(`(?m)^\s*[0-9a-f]{4,}\s+(\S+)\s+filter\b`)
var providerComponentRe = regexp.MustCompile(`(?m)^\s*[0-9a-f]{4,}\s+(\S+/\S*)\s*$`)
var sectionEndRe = regexp.MustCompile(`\n    [A-Z]`)

// section returns the dumpsys output after a 4-space section header until
// the next 4-space header.
func section(out, marker string) string {
	i := strings.Index(out, marker)
	if i < 0 {
		return ""
	}
	rest := out[i+len(marker):]
	end := len(rest)
	if m := sectionEndRe.FindStringIndex(rest); m != nil {
		end = m[0]
	}
	return rest[:end]
}

// parseDangerousPermissions extracts the requested dangerous permissions
// from the "requested permissions:" block.
func parseDangerousPermissions(out string) []string {
	marker := "requested permissions:"
	i := strings.Index(out, marker)
	if i < 0 {
		return nil
	}
	var res []string
	started := false
	for _, line := range strings.Split(out[i+len(marker):], "\n") {
		line = strings.TrimSpace(line)
		if !started {
			if line != "" {
				started = true
			} else {
				continue
			}
		}
		if line == "" || !strings.HasPrefix(line, "android.permission.") {
			break
		}
		perm := strings.SplitN(line, ",", 2)[0]
		if dangerousPermSet[perm] {
			res = append(res, perm)
		}
	}
	sort.Strings(res)
	return res
}

// parseSurface builds an AppSurface from dumpsys package output.
func parseSurface(pkg, out string) *AppSurface {
	a := &AppSurface{Package: pkg}
	a.Debuggable = strings.Contains(out, "DEBUGGABLE")
	a.AllowBackup = strings.Contains(out, "ALLOW_BACKUP")
	a.DangerousPermissions = parseDangerousPermissions(out)

	collect := func(marker string, re *regexp.Regexp) []string {
		sec := section(out, marker)
		seen := map[string]bool{}
		var res []string
		for _, m := range re.FindAllStringSubmatch(sec, -1) {
			c := m[1]
			if !seen[c] {
				seen[c] = true
				res = append(res, c)
			}
		}
		sort.Strings(res)
		return res
	}

	a.ExportedActivities = collect("Activity Resolver Table:", resolverComponentRe)
	a.ExportedServices = collect("Service Resolver Table:", resolverComponentRe)
	a.ExportedReceivers = collect("Receiver Resolver Table:", resolverComponentRe)
	a.ExportedProviders = collect("ContentProvider Authorities:", providerComponentRe)
	return a
}

// ScanAppSurface dumps one package and parses its attack surface.
func ScanAppSurface(serial, pkg string) (*AppSurface, error) {
	out, err := Run(serial, "shell", "dumpsys", "package", pkg)
	if err != nil {
		return nil, err
	}
	return parseSurface(pkg, out), nil
}

// ScanAllSurfaces scans every user app, ranked by finding count.
// progress (optional) is called before each package scan.
func ScanAllSurfaces(serial string, progress func(done, total int, pkg string)) ([]AppSurface, error) {
	pkgs, err := UserPackages(serial)
	if err != nil {
		return nil, err
	}
	var res []AppSurface
	for i, pkg := range pkgs {
		pkg = strings.TrimSpace(pkg)
		pkg = strings.TrimPrefix(pkg, "package:")
		if pkg == "" {
			continue
		}
		if progress != nil {
			progress(i, len(pkgs), pkg)
		}
		if s, err := ScanAppSurface(serial, pkg); err == nil {
			res = append(res, *s)
		}
	}
	sort.Slice(res, func(i, j int) bool {
		return len(res[i].Issues()) > len(res[j].Issues())
	})
	return res, nil
}

// Issues lists the human-readable security findings for the app.
func (a *AppSurface) Issues() []string {
	var issues []string
	if a.Debuggable {
		issues = append(issues, "debuggable — runtime memory inspectable & hookable via jdwp/Frida")
	}
	if a.AllowBackup {
		issues = append(issues, "allowBackup — private app data extractable with adb backup")
	}
	for _, c := range a.ExportedActivities {
		issues = append(issues, "exported activity "+c+" — launchable via am start, can bypass UI/auth flows")
	}
	for _, c := range a.ExportedServices {
		issues = append(issues, "exported service "+c+" — invocable via am startservice")
	}
	for _, c := range a.ExportedReceivers {
		issues = append(issues, "exported receiver "+c+" — triggerable with crafted broadcasts")
	}
	for _, c := range a.ExportedProviders {
		issues = append(issues, "exported provider "+c+" — queryable via content query")
	}
	return issues
}

// Report renders a human-readable attack-surface summary.
func (a *AppSurface) Report() string {
	var b strings.Builder
	issues := a.Issues()
	status := "✔ clean — no obvious exposure"
	if len(issues) > 0 {
		status = fmt.Sprintf("⚠ %d finding(s)", len(issues))
	}
	fmt.Fprintf(&b, "%s  %s\n", a.Package, status)
	for _, is := range issues {
		fmt.Fprintf(&b, "    ⚠ %s\n", is)
	}
	return b.String()
}
