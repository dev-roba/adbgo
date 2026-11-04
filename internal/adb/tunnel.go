package adb

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// Tunnel is a persistent SSH reverse tunnel exposing a local port on a
// VPS, so adbgo services (share page, web panel) get a stable public
// address without any third-party tunnel service.
type Tunnel struct {
	Name       string `json:"name"`
	Via        string `json:"via"`
	LocalPort  int    `json:"local_port"`
	RemotePort int    `json:"remote_port"`
	Created    string `json:"created"`
}

// tunnelFile is the persisted tunnel list (~/.config/adbgo/tunnels.json).
type tunnelFile struct {
	Tunnels []Tunnel `json:"tunnels"`
}

func tunnelFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "tunnels.json"
	}
	return filepath.Join(home, ".config", "adbgo", "tunnels.json")
}

// LoadTunnels reads the saved tunnel configs.
func LoadTunnels() []Tunnel {
	tf := &tunnelFile{}
	data, err := os.ReadFile(tunnelFilePath())
	if err == nil {
		_ = json.Unmarshal(data, tf)
	}
	return tf.Tunnels
}

func saveTunnels(tunnels []Tunnel) error {
	if err := os.MkdirAll(filepath.Dir(tunnelFilePath()), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(tunnelFile{Tunnels: tunnels}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(tunnelFilePath(), data, 0o600)
}

var (
	tunnelMu    sync.Mutex
	tunnelProcs = map[string]*exec.Cmd{}
	tunnelStops = map[string]chan struct{}{}
)

// TunnelUp saves the tunnel config, then runs its SSH reverse tunnel
// with auto-heal: if the connection drops, it reconnects with
// exponential backoff. It blocks until TunnelDown(name) is called.
func TunnelUp(t Tunnel) error {
	TunnelDown(t.Name)
	stopCh := make(chan struct{})
	tunnelMu.Lock()
	tunnelStops[t.Name] = stopCh
	tunnelMu.Unlock()

	tunnels := LoadTunnels()
	found := false
	for i := range tunnels {
		if tunnels[i].Name == t.Name {
			tunnels[i] = t
			found = true
		}
	}
	if !found {
		tunnels = append(tunnels, t)
	}
	if err := saveTunnels(tunnels); err != nil {
		return err
	}

	backoff := 2 * time.Second
	for {
		select {
		case <-stopCh:
			return nil
		default:
		}
		cmd := exec.Command("ssh", "-N",
			"-o", "StrictHostKeyChecking=no",
			"-o", "ServerAliveInterval=30",
			"-o", "ExitOnForwardFailure=yes",
			"-R", fmt.Sprintf("%d:localhost:%d", t.RemotePort, t.LocalPort),
			t.Via)
		tunnelMu.Lock()
		tunnelProcs[t.Name] = cmd
		tunnelMu.Unlock()
		_ = cmd.Run()
		select {
		case <-stopCh:
			return nil
		default:
		}
		time.Sleep(backoff)
		if backoff < 30*time.Second {
			backoff *= 2
		}
	}
}

// TunnelDown stops a running tunnel by name (no-op when not running).
func TunnelDown(name string) {
	tunnelMu.Lock()
	stopCh, ok := tunnelStops[name]
	if ok {
		close(stopCh)
		delete(tunnelStops, name)
	}
	cmd := tunnelProcs[name]
	delete(tunnelProcs, name)
	tunnelMu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}

// TunnelRunning reports whether a tunnel process is currently alive.
func TunnelRunning(name string) bool {
	tunnelMu.Lock()
	cmd := tunnelProcs[name]
	tunnelMu.Unlock()
	return cmd != nil && cmd.ProcessState == nil
}

// GenFRPConfig writes an frpc config mirroring a tunnel through an frps
// server — for setups where both sides are behind NAT.
func GenFRPConfig(name, serverAddr string, localPort, remotePort int) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	path := filepath.Join(home, ".config", "adbgo", "frpc-"+name+".ini")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	cfg := "[common]\nserver_addr = " + serverAddr + "\nserver_port = 7000\n\n[adbgo-" + name + "]\ntype = tcp\nlocal_ip = 127.0.0.1\nlocal_port = " + fmt.Sprint(localPort) + "\nremote_port = " + fmt.Sprint(remotePort) + "\n"
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
