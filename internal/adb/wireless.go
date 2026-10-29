package adb

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

var ipRe = regexp.MustCompile(`(\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3})`)

// GetDeviceIP returns the Wi-Fi IP of the device (from wlan0, then default route).
func GetDeviceIP(serial string) (string, error) {
	out, err := Run(serial, "shell", "ip", "-f", "inet", "addr", "show", "wlan0")
	if err == nil {
		if m := ipRe.FindStringSubmatch(out); m != nil && !strings.HasPrefix(m[1], "127.") {
			return m[1], nil
		}
	}
	// Fallback: default route source address
	out, _ = Run(serial, "shell", "ip", "route")
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, " wlan0 ") && strings.Contains(line, " src ") {
			if m := ipRe.FindStringSubmatch(line); m != nil {
				return m[1], nil
			}
		}
	}
	return "", fmt.Errorf("could not determine device Wi-Fi IP (is the device on Wi-Fi?)")
}

// EnableTCPIP switches the device's adbd to TCP/IP mode on port 5555.
func EnableTCPIP(serial string) error {
	_, err := Run(serial, "tcpip", "5555")
	return err
}

// ConnectWireless enables tcpip mode, waits for the daemon to restart and
// connects to the device over Wi-Fi. Returns the new wireless serial
// ("ip:5555"). Note: the USB cable can be unplugged once connected.
func ConnectWireless(serial string) (string, error) {
	ip, err := GetDeviceIP(serial)
	if err != nil {
		return "", err
	}

	if err := EnableTCPIP(serial); err != nil {
		return "", fmt.Errorf("failed to enable tcpip mode: %w", err)
	}

	// adbd restarts after `adb tcpip`; give it a moment before connecting
	time.Sleep(3 * time.Second)

	wirelessSerial := ip + ":5555"
	out, err := RunRaw("connect", wirelessSerial)
	_ = out
	if err != nil {
		// Retry once; adbd can be slow to come back
		time.Sleep(3 * time.Second)
		out, err = RunRaw("connect", wirelessSerial)
		if err != nil {
			return "", fmt.Errorf("could not connect to %s: %w", wirelessSerial, err)
		}
	}
	return wirelessSerial, nil
}

// DisconnectWireless disconnects a wireless (tcpip) device.
func DisconnectWireless(serial string) error {
	_, err := RunRaw("disconnect", serial)
	return err
}
