package auth

import (
	"fmt"
	"time"

	"github.com/dev-roba/adbgo/internal/config"
	"golang.org/x/crypto/bcrypt"
)

const SessionTimeout = 30 * time.Minute

const MinPasswordLength = 8

type Session struct {
	Device     *config.Device
	LoggedInAt time.Time
	GodMode    bool
}

func (s *Session) IsExpired() bool {
	return time.Since(s.LoggedInAt) > SessionTimeout
}

func (s *Session) Refresh() {
	s.LoggedInAt = time.Now()
}

func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(hash), nil
}

func CheckPassword(hash, password string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func ValidatePasswordStrength(password string) error {
	if len(password) < MinPasswordLength {
		return fmt.Errorf("password too short (min %d characters)", MinPasswordLength)
	}
	return nil
}

// Signup registers a device. If the serial is already registered, an error is
// returned so an existing registration can never be replaced by accident.
func Signup(cfg *config.Config, serial, name, password string) (*Session, error) {
	if name == "" {
		name = serial
	}

	if err := ValidatePasswordStrength(password); err != nil {
		return nil, err
	}

	if cfg.FindBySerial(serial) != nil {
		return nil, fmt.Errorf("device %s is already registered — remove it first from Device Management if you want to re-register it", serial)
	}

	hash, err := HashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("failed to hash password: %w", err)
	}

	dev := config.Device{
		Serial:         serial,
		Name:           name,
		Password:       hash,
		UnlockPassword: password,
		Created:        time.Now(),
	}

	cfg.AddDevice(dev)
	if err := cfg.Save(); err != nil {
		return nil, fmt.Errorf("failed to save config: %w", err)
	}

	return &Session{
		Device:     cfg.FindBySerial(serial),
		LoggedInAt: time.Now(),
		GodMode:    false,
	}, nil
}

// LoginWithoutPassword creates an authenticated session directly without asking for a password.
func LoginWithoutPassword(cfg *config.Config, serial string) (*Session, error) {
	dev := cfg.FindBySerial(serial)
	if dev == nil {
		return nil, fmt.Errorf("device not registered")
	}

	return &Session{
		Device:     dev,
		LoggedInAt: time.Now(),
		GodMode:    false,
	}, nil
}

func Login(cfg *config.Config, serial, password string) (*Session, error) {
	dev := cfg.FindBySerial(serial)
	if dev == nil {
		return nil, fmt.Errorf("device not registered")
	}

	if !CheckPassword(dev.Password, password) {
		return nil, fmt.Errorf("incorrect password")
	}

	return &Session{
		Device:     dev,
		LoggedInAt: time.Now(),
		GodMode:    false,
	}, nil
}
