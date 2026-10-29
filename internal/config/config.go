package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"fmt"
	"time"
)

type Device struct {
	Serial   string    `json:"serial"`
	Name     string    `json:"name"`
	Password string    `json:"password"` // bcrypt hash
	Created  time.Time `json:"created"`
}

type Config struct {
	Devices []Device `json:"devices"`
}

func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "adbgo")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0700); err != nil {
		return "", fmt.Errorf("secure config directory permissions: %w", err)
	}
	return dir, nil
}

func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "devices.json"), nil
}

func Load() (*Config, error) {
	path, err := ConfigPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &Config{Devices: []Device{}}, nil
		}
		return nil, err
	}
	if err := os.Chmod(path, 0600); err != nil {
		return nil, fmt.Errorf("secure config file permissions: %w", err)
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) Save() error {
	path, err := ConfigPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), ".devices-*.tmp")
	if err != nil { return err }
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0600); err != nil { _ = tmp.Close(); return err }
	if _, err := tmp.Write(data); err != nil { _ = tmp.Close(); return err }
	if err := tmp.Sync(); err != nil { _ = tmp.Close(); return err }
	if err := tmp.Close(); err != nil { return err }
	if err := os.Rename(tmpName, path); err != nil { return fmt.Errorf("replace device registry: %w", err) }
	return nil
}

func (c *Config) FindBySerial(serial string) *Device {
	for i := range c.Devices {
		if c.Devices[i].Serial == serial {
			return &c.Devices[i]
		}
	}
	return nil
}

func (c *Config) AddDevice(d Device) {
	// Replace if exists
	for i := range c.Devices {
		if c.Devices[i].Serial == d.Serial {
			c.Devices[i] = d
			return
		}
	}
	c.Devices = append(c.Devices, d)
}

func (c *Config) RemoveDevice(serial string) {
	filtered := c.Devices[:0]
	for _, d := range c.Devices {
		if d.Serial != serial {
			filtered = append(filtered, d)
		}
	}
	c.Devices = filtered
}
