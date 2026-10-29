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

// ConnectDirect connects to an arbitrary IP:port (for Android 11+ wireless debugging).
func ConnectDirect(ipPort string) error {
	out, err := RunRaw("connect", ipPort)
	if err != nil {
		return err
	}
	if strings.Contains(out, "failed to authenticate") || strings.Contains(out, "cannot connect") {
		return fmt.Errorf("%s", strings.TrimSpace(out))
	}
	return nil
}

// PairWireless pairs with an Android 11+ device using a 6-digit pairing code.
func PairWireless(ipPort, code string) error {
	out, err := RunRaw("pair", ipPort, code)
	if err != nil {
		return fmt.Errorf("pairing failed: %w\n%s", err, out)
	}
	if strings.Contains(out, "Failed") {
		return fmt.Errorf("pairing failed: %s", strings.TrimSpace(out))
	}
	return nil
}

// DiscoverMDNS checks for wireless debugging targets advertising over mDNS.
func DiscoverMDNS() ([]string, error) {
	out, err := RunRaw("mdns", "services")
	if err != nil {
		return nil, err
	}
	var targets []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "_adb-tls-connect._tcp") || strings.Contains(line, "_adb._tcp") {
			parts := strings.Fields(line)
			if len(parts) >= 3 {
				// The last column is usually the ip:port
				targets = append(targets, parts[len(parts)-1])
			}
		}
	}
	return targets, nil
}

// DisconnectWireless disconnects a wireless (tcpip) device.
func DisconnectWireless(serial string) error {
	_, err := RunRaw("disconnect", serial)
	return err
}
