package adb

import (
	"strings"
	"testing"
)

const sampleDumpsys = "Package [com.example.bank] (5845c30):\n" +
	"  pkgFlags=[ DEBUGGABLE HAS_CODE ALLOW_CLEAR_USER_DATA ALLOW_BACKUP ]\n" +
	"  requested permissions:\n" +
	"    android.permission.INTERNET\n" +
	"    android.permission.CAMERA\n" +
	"    android.permission.READ_CONTACTS\n" +
	"    android.permission.SET_WALLPAPER\n" +
	"  install permissions:\n" +
	"    android.permission.INTERNET: granted=true\n" +
	"  Activity Resolver Table:\n" +
	"    Non-Data Actions:\n" +
	"      android.intent.action.MAIN:\n" +
	"        5845c30 com.example.bank/.MainActivity filter 5845c34\n" +
	"          Action: \"android.intent.action.MAIN\"\n" +
	"  Service Resolver Table:\n" +
	"    Non-Data Actions:\n" +
	"      com.example.bank.PAY:\n" +
	"        5845c40 com.example.bank/.PayService filter 5845c41\n" +
	"  Receiver Resolver Table:\n" +
	"    Non-Data Actions:\n" +
	"      android.intent.action.BOOT_COMPLETED:\n" +
	"        5845c50 com.example.bank/.BootReceiver filter 5845c51\n" +
	"  ContentProvider Authorities:\n" +
	"    com.example.bank.files:\n" +
	"      5845c60 com.example.bank/.FilesProvider\n" +
	"  Splits:\n" +
	"    Base APK:\n"

func TestParseSurface(t *testing.T) {
	a := parseSurface("com.example.bank", sampleDumpsys)
	if !a.Debuggable {
		t.Error("debuggable not detected")
	}
	if !a.AllowBackup {
		t.Error("allowBackup not detected")
	}
	if len(a.ExportedActivities) != 1 || a.ExportedActivities[0] != "com.example.bank/.MainActivity" {
		t.Errorf("exported activities = %v", a.ExportedActivities)
	}
	if len(a.ExportedServices) != 1 || a.ExportedServices[0] != "com.example.bank/.PayService" {
		t.Errorf("exported services = %v", a.ExportedServices)
	}
	if len(a.ExportedReceivers) != 1 || a.ExportedReceivers[0] != "com.example.bank/.BootReceiver" {
		t.Errorf("exported receivers = %v", a.ExportedReceivers)
	}
	if len(a.ExportedProviders) != 1 || a.ExportedProviders[0] != "com.example.bank/.FilesProvider" {
		t.Errorf("exported providers = %v", a.ExportedProviders)
	}
	if len(a.DangerousPermissions) != 2 {
		t.Errorf("dangerous permissions = %v, want CAMERA & READ_CONTACTS", a.DangerousPermissions)
	}
}

func TestSurfaceIssues(t *testing.T) {
	a := parseSurface("com.example.bank", sampleDumpsys)
	issues := strings.Join(a.Issues(), "\n")
	for _, want := range []string{"debuggable", "allowBackup", "exported activity", "exported service", "exported receiver", "exported provider"} {
		if !strings.Contains(issues, want) {
			t.Errorf("issues missing %q: %s", want, issues)
		}
	}
}

func TestSurfaceClean(t *testing.T) {
	a := parseSurface("com.example.good", "Package [com.example.good]:\n  pkgFlags=[ HAS_CODE ]\n")
	if len(a.Issues()) != 0 {
		t.Errorf("clean app has issues: %v", a.Issues())
	}
	if !strings.Contains(a.Report(), "clean") {
		t.Errorf("clean report = %q", a.Report())
	}
}
