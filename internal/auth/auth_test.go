package auth

import (
	"testing"
	"time"

	"github.com/dev-roba/adbgo/internal/config"
)

func TestPasswordStrength(t *testing.T) {
	if err := ValidatePasswordStrength("short"); err == nil {
		t.Error("expected error for password < 8 chars, got nil")
	}
	if err := ValidatePasswordStrength("validpassword123"); err != nil {
		t.Errorf("expected nil for valid password, got: %v", err)
	}
}

func TestHashAndCheckPassword(t *testing.T) {
	pw := "secretP@ssw0rd"
	hash, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword failed: %v", err)
	}

	if !CheckPassword(hash, pw) {
		t.Error("CheckPassword failed with correct password")
	}

	if CheckPassword(hash, "wrongpassword") {
		t.Error("CheckPassword succeeded with wrong password")
	}
}

func TestSessionExpiryAndRefresh(t *testing.T) {
	s := &Session{
		LoggedInAt: time.Now().Add(-31 * time.Minute),
	}
	if !s.IsExpired() {
		t.Error("session should be expired after 31 minutes")
	}

	s.Refresh()
	if s.IsExpired() {
		t.Error("session should not be expired immediately after Refresh()")
	}
}

func TestSignupAndLogin(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}

	serial := "DEV123456"
	name := "Pixel Test"
	pw := "securepassword"

	sess, err := Signup(cfg, serial, name, pw)
	if err != nil {
		t.Fatalf("Signup failed: %v", err)
	}
	if sess.Device.Serial != serial || sess.Device.Name != name {
		t.Errorf("unexpected session device: %+v", sess.Device)
	}

	// Duplicate signup should fail
	if _, err := Signup(cfg, serial, "Duplicate", pw); err == nil {
		t.Error("Signup should fail when registering an existing serial")
	}

	// Login with correct password
	loginSess, err := Login(cfg, serial, pw)
	if err != nil {
		t.Fatalf("Login failed with correct password: %v", err)
	}
	if loginSess.Device.Serial != serial {
		t.Errorf("unexpected login device serial: %s", loginSess.Device.Serial)
	}

	// Login with incorrect password
	if _, err := Login(cfg, serial, "wrongpass"); err == nil {
		t.Error("Login should fail with wrong password")
	}

	// Login with unregistered serial
	if _, err := Login(cfg, "UNKNOWN_SERIAL", pw); err == nil {
		t.Error("Login should fail for unregistered serial")
	}
}
