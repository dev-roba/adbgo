package auth

import (
	"testing"

	"adbgo/internal/config"
)

func TestSignupRejectsWeakPassword(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	cfg := &config.Config{}
	if _, err := Signup(cfg, "serial", "Phone", "short"); err == nil {
		t.Fatal("Signup accepted a password shorter than 10 characters")
	}
	if len(cfg.Devices) != 0 {
		t.Fatal("Signup modified config after rejecting a weak password")
	}
}

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(hash, "correct horse battery staple") {
		t.Fatal("CheckPassword rejected the correct password")
	}
	if CheckPassword(hash, "incorrect password") {
		t.Fatal("CheckPassword accepted an incorrect password")
	}
}
