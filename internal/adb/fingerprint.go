package adb

import (
	"fmt"
	"strings"
	"time"
)

// Fingerprint is a security-focused device profile built from read-only
// shell queries (getprop, getenforce, uname).
type Fingerprint struct {
	Serial           string   `json:"serial"`
	Brand            string   `json:"brand"`
	Model            string   `json:"model"`
	Device           string   `json:"device"`
	AndroidVersion   string   `json:"android_version"`
	SDK              string   `json:"sdk"`
	SecurityPatch    string   `json:"security_patch"`
	BuildID          string   `json:"build_id"`
	BuildFingerprint string   `json:"build_fingerprint"`
	BuildTags        string   `json:"build_tags"`
	KernelVersion    string   `json:"kernel_version"`
	SELinux          string   `json:"selinux"`
	EncryptionState  string   `json:"encryption_state"`
	VerifiedBoot     string   `json:"verified_boot_state"`
	RootIndicators   []string `json:"root_indicators"`
	Rooted           bool     `json:"rooted"`
}

// CollectFingerprint profiles a device with read-only commands only.
func CollectFingerprint(serial string) (*Fingerprint, error) {
	f := &Fingerprint{Serial: serial}

	props := map[string]string{}
	if out, err := Run(serial, "shell", "getprop"); err == nil {
		props = ParseGetprop(out)
	}
	f.Brand = props["ro.product.brand"]
	f.Model = props["ro.product.model"]
	f.Device = props["ro.product.device"]
	f.AndroidVersion = props["ro.build.version.release"]
	f.SDK = props["ro.build.version.sdk"]
	f.SecurityPatch = props["ro.build.version.security_patch"]
	f.BuildID = props["ro.build.id"]
	f.BuildFingerprint = props["ro.build.fingerprint"]
	f.BuildTags = props["ro.build.tags"]
	f.EncryptionState = props["ro.crypto.state"]
	f.VerifiedBoot = props["ro.boot.verifiedbootstate"]

	if out, err := Run(serial, "shell", "getenforce"); err == nil {
		f.SELinux = strings.TrimSpace(out)
	}
	if out, err := Run(serial, "shell", "uname", "-r"); err == nil {
		f.KernelVersion = strings.TrimSpace(out)
	}

	// Root indicators (each check is harmless on non-rooted devices).
	if out, err := Run(serial, "shell", "which", "su"); err == nil {
		if p := strings.TrimSpace(out); p != "" {
			f.RootIndicators = append(f.RootIndicators, "su binary at "+p)
		}
	}
	if strings.EqualFold(f.BuildTags, "test-keys") {
		f.RootIndicators = append(f.RootIndicators, "build signed with test-keys (custom/unsigned ROM)")
	}
	if props["ro.debuggable"] == "1" {
		f.RootIndicators = append(f.RootIndicators, "ro.debuggable=1 (userdebug/eng build)")
	}
	if out, err := Run(serial, "shell", "getprop", "ro.magisk.version"); err == nil {
		if v := strings.TrimSpace(out); v != "" {
			f.RootIndicators = append(f.RootIndicators, "Magisk "+v)
		}
	}
	f.Rooted = len(f.RootIndicators) > 0
	return f, nil
}

// ParseGetprop converts getprop output ([key]: [value] lines) into a map.
func ParseGetprop(out string) map[string]string {
	m := map[string]string{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "[") || !strings.Contains(line, "]: [") {
			continue
		}
		line = strings.TrimSuffix(line, "]")
		parts := strings.SplitN(line, "]: [", 2)
		key := strings.TrimPrefix(parts[0], "[")
		val := ""
		if len(parts) > 1 {
			val = parts[1]
		}
		m[key] = val
	}
	return m
}

// PatchAge returns the age of the security patch, if the date parses.
func (f *Fingerprint) PatchAge() (time.Duration, bool) {
	t, err := time.Parse("2006-01-02", f.SecurityPatch)
	if err != nil {
		return 0, false
	}
	return time.Since(t), true
}

// RiskVerdict summarizes the device's attack posture.
func (f *Fingerprint) RiskVerdict() string {
	var risks []string
	if age, ok := f.PatchAge(); ok {
		if months := int(age.Hours() / 24 / 30); months >= 6 {
			risks = append(risks, fmt.Sprintf("security patch ~%d months old (known CVEs likely unpatched)", months))
		}
	}
	if strings.EqualFold(f.SELinux, "permissive") || strings.EqualFold(f.SELinux, "disabled") {
		risks = append(risks, "SELinux is "+f.SELinux)
	}
	if f.Rooted {
		risks = append(risks, "root present ("+strings.Join(f.RootIndicators, "; ")+")")
	}
	if strings.EqualFold(f.VerifiedBoot, "orange") || strings.EqualFold(f.VerifiedBoot, "red") {
		risks = append(risks, "verified boot state is "+f.VerifiedBoot+" (bootloader modified/unlocked)")
	}
	if strings.EqualFold(f.EncryptionState, "unencrypted") {
		risks = append(risks, "userdata is unencrypted")
	}
	if len(risks) == 0 {
		return "LOW — hardened, up-to-date device posture"
	}
	return "ELEVATED — " + strings.Join(risks, "; ")
}

// Report renders a human-readable device fingerprint.
func (f *Fingerprint) Report() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Device:        %s %s (codename %s)\n", f.Brand, f.Model, f.Device)
	fmt.Fprintf(&b, "Android:       %s (SDK %s), security patch %s\n", f.AndroidVersion, f.SDK, f.SecurityPatch)
	fmt.Fprintf(&b, "Build:         %s\n", f.BuildID)
	fmt.Fprintf(&b, "Fingerprint:   %s\n", f.BuildFingerprint)
	fmt.Fprintf(&b, "Kernel:        %s\n", f.KernelVersion)
	fmt.Fprintf(&b, "SELinux:       %s\n", f.SELinux)
	fmt.Fprintf(&b, "Encryption:    %s\n", f.EncryptionState)
	fmt.Fprintf(&b, "Verified boot: %s\n", f.VerifiedBoot)
	if len(f.RootIndicators) > 0 {
		fmt.Fprintf(&b, "Root:          PRESENT\n")
		for _, r := range f.RootIndicators {
			fmt.Fprintf(&b, "               - %s\n", r)
		}
	} else {
		fmt.Fprintf(&b, "Root:          no indicators found\n")
	}
	return b.String()
}
