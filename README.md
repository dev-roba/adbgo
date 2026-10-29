# adbgo

**A lightweight, terminal-first Android device toolkit built around ADB.** Designed for Linux (including Arch Linux / Omarchy), with Go-based builds for Linux, macOS, and Windows.

> adbgo is a convenience layer over Android's debugging tools, not a root exploit or an FRP/lock-screen bypass. Use it only with devices you own or are explicitly authorized to administer.

## Features

- Password-based local login and registered-device profiles
- Interactive device selection and registration management (rename/remove)
- USB-to-Wi-Fi ADB connection helper for legacy TCP/IP debugging
- Live Logcat with priority selection and real process-ID filtering for a running package
- APK installation with a local file chooser when `zenity` or `kdialog` is installed
- Interactively browse `/sdcard` directories and push/pull selected files or folders
- Bounded screen recording with automatic pull to the computer
- Multi-device diagnostics and explicitly confirmed reboot actions with per-device results
- scrcpy launch, screenshots, device controls, and app/package management
- Extra-dangerous actions gated behind GOD MODE, password re-entry, and explicit confirmation

## Requirements

Install Go and Android platform tools. On Arch Linux:

```bash
sudo pacman -S go android-tools scrcpy
```

Optional native file chooser:

```bash
sudo pacman -S zenity
# or: sudo pacman -S kdialog
```

## Build and run

```bash
git clone https://github.com/dev-roba/adbgo.git
cd adbgo
go mod tidy
go test ./...
go vet ./...
go build -o adbgo .
./adbgo
./adbgo --version
```

## Wireless ADB

For older Android versions or devices that support legacy TCP/IP mode:

1. Connect and authorize the phone over USB.
2. Keep the computer and phone on the same trusted Wi-Fi network.
3. Choose **Wireless ADB**, select the USB-connected device, and enter the phone's IP address and port.
4. Verify the device appears in `adb devices` before disconnecting USB.

The legacy `adb tcpip 5555` workflow is not supported by every Android build. On Android 11+, prefer **Developer options → Wireless debugging**, use Android's pairing flow, and connect using the IP address and port shown by the phone. Do not expose ADB ports to public or untrusted networks.

## Releases

Version tags such as `v0.1.0` trigger the GoReleaser workflow and attach archives plus checksums to a GitHub Release. CI runs formatting, vetting, tests, and builds.

## Configuration

The local device registry is stored at:

```
~/.config/adbgo/devices.json
```

The config directory is created with owner-only permissions, and the registry file is written with owner-only permissions. Treat this file as sensitive because it contains password hashes and device identifiers.

## Safety notes

- ADB operations require Android debugging authorization; adbgo does not grant root.
- GOD MODE is an application-level warning gate, not Android root access.
- Operations that erase data or alter bootloader state can permanently destroy data or make a device unbootable. Confirm the target serial and manufacturer-supported prerequisites before proceeding.
- Android security protections such as Factory Reset Protection should be handled through the device owner's account or the manufacturer's official recovery process. adbgo does not promise to bypass them.
- Wireless ADB should only be enabled on trusted networks and disabled when no longer needed.
- Review commands before running raw ADB/Fastboot operations.

## Contributing

Issues and pull requests are welcome. Please include reproduction steps, device/Android versions, and relevant sanitized logs. Never post passwords, auth tokens, or private device data.

## License

MIT. See [LICENSE](LICENSE).
