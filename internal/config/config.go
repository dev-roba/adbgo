package config

import (
	"encoding/json"
	"os"
	"path/filepath"
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

	// Write atomically so a crash never corrupts the config
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *Config) FindBySerial(serial string) *Device {
	for i := range c.Devices {
		if c.Devices[i].Serial == serial {
			return &c.Devices[i]
		}
	}
	return nil
}

// AddDevice adds or replaces a device registration. Returns true if an
// existing registration was overwritten.
func (c *Config) AddDevice(d Device) bool {
	for i := range c.Devices {
		if c.Devices[i].Serial == d.Serial {
			c.Devices[i] = d
			return true
		}
	}
	c.Devices = append(c.Devices, d)
	return false
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

// RenameDevice updates the friendly name of a registered device.
func (c *Config) RenameDevice(serial, name string) bool {
	for i := range c.Devices {
		if c.Devices[i].Serial == serial {
			c.Devices[i].Name = name
			return true
		}
	}
	return false
}
