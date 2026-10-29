package config

import (
	"testing"
	"time"
)

func TestSaveLoadRenameRemove(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	d := Device{Serial: "ABC123", Name: "Test Phone", Password: "hash", Created: time.Now()}
	if cfg.AddDevice(d) {
		t.Error("AddDevice should report false for a new device")
	}
	if err := cfg.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	loaded, err := Load()
	if err != nil {
		t.Fatalf("Load after save: %v", err)
	}
	if len(loaded.Devices) != 1 || loaded.Devices[0].Serial != "ABC123" {
		t.Fatalf("unexpected loaded config: %+v", loaded.Devices)
	}

	if !loaded.RenameDevice("ABC123", "Renamed") {
		t.Error("RenameDevice should succeed for existing device")
	}
	if loaded.RenameDevice("MISSING", "x") {
		t.Error("RenameDevice should fail for unknown device")
	}
	if err := loaded.Save(); err != nil {
		t.Fatalf("Save after rename: %v", err)
	}

	loaded.RemoveDevice("ABC123")
	if err := loaded.Save(); err != nil {
		t.Fatalf("Save after remove: %v", err)
	}
	final, err := Load()
	if err != nil {
		t.Fatalf("Load final: %v", err)
	}
	if len(final.Devices) != 0 {
		t.Fatalf("expected 0 devices, got %d", len(final.Devices))
	}
}
