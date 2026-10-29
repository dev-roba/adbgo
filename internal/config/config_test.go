package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveLoadAndRemoveDevice(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &Config{Devices: []Device{{
		Serial: "test-serial",
		Name: "Test phone",
		Password: "$2a$12$example-hash",
		Created: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}}}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	path, err := ConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0600 {
		t.Fatalf("config mode = %04o, want 0600", info.Mode().Perm())
	}
	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(loaded.Devices) != 1 || loaded.Devices[0].Serial != "test-serial" {
		t.Fatalf("unexpected loaded config: %#v", loaded.Devices)
	}
	loaded.RemoveDevice("test-serial")
	if len(loaded.Devices) != 0 {
		t.Fatalf("RemoveDevice left entries: %#v", loaded.Devices)
	}
	if err := loaded.Save(); err != nil {
		t.Fatalf("Save() after remove error = %v", err)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("config directory missing: %v", err)
	}
}

func TestFindBySerialReturnsMatchingDevice(t *testing.T) {
	cfg := &Config{Devices: []Device{{Serial: "a", Name: "A"}, {Serial: "b", Name: "B"}}}
	got := cfg.FindBySerial("b")
	if got == nil || got.Name != "B" {
		t.Fatalf("FindBySerial(%q) = %#v, want device B", "b", got)
	}
	if cfg.FindBySerial("missing") != nil {
		t.Fatal("FindBySerial for missing serial should return nil")
	}
}
