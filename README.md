# adbgo

Fast terminal power tool for ADB: wireless ADB, live logcat, file manager, APK
sideloading, screen recording, multi-device mode — plus a login system,
GOD MODE, and nuclear options for devices you own.

Built on Linux (Arch/Omarchy first-class) with Go + [huh](https://github.com/charmbracelet/huh) + [lipgloss](https://github.com/charmbracelet/lipgloss). Works on any Linux/macOS with Go + android-tools.

## Features

- **Login / Signup** system tied to device serial + password (bcrypt, min 8 chars)
- **Device Management** — rename or remove registered devices, export/import
- 30-minute session timeout
- Colorful UI (green normal / red GOD MODE)
- **scrcpy** integration
- Device control (reboot, recovery, bootloader, screenshots, wake/lock...)
- **Wireless ADB** — `adb tcpip 5555` → connect over Wi-Fi, unplug the cable
- **Live logcat viewer** — filter by package and priority, streamed to your terminal
- **APK sideload** — file picker + streamed install progress (`adb install -r`)
- **File manager** — browse `/sdcard`, push/pull, mkdir, delete
- **Screen recording** — `screenrecord` with auto-pull to your PC when stopped
- **Multi-device mode** — run actions against several devices at once
- **GOD MODE** → password-protected, unlocks the full Danger Zone
- Nuclear options: factory reset, bootloader unlock/lock (waits for fastboot),
  remove screen lock, FRP attempts, raw adb/fastboot
- Every dangerous action requires typing `YES` (and the serial for the worst ones)

## Requirements

```bash
# Arch / Omarchy
sudo pacman -S go android-tools scrcpy

# Debian / Ubuntu
sudo apt install golang-go adb scrcpy
```

## Install

Grab a prebuilt binary from [Releases](https://github.com/dev-roba/adbgo/releases),
or build from source:

```bash
git clone https://github.com/dev-roba/adbgo
cd adbgo
go build -o adbgo .
```

Optional:

```bash
cp adbgo ~/.local/bin/   # or any directory in your PATH
```

## Usage

```bash
./adbgo
```

First time → Signup → select your device from `adb devices` → set a password (8+ chars).

Next times → Login with the registered device.

### Wireless ADB (recommended QoL)

Connect your phone over USB once → **File & System → Wireless ADB → Connect**.
adbgo enables tcpip mode, finds the phone's Wi-Fi IP and connects. You can then
unplug the cable and keep using scrcpy, logcat and file transfer over Wi-Fi.
Register the wireless serial (`ip:5555`) via Signup to log in without a cable at all.

## Config location

`~/.config/adbgo/devices.json`

You can copy this file to move your registered devices to another machine
(or use the built-in Export/Import).

## Development

```bash
go test ./...   # unit tests
go vet ./...    # static checks (CI runs gofmt + vet + build + test)
```

Releases are cut with GoReleaser — push a `v*` tag and GitHub Actions
publishes binaries for Linux, macOS and Windows (amd64 + arm64).

## Warning

GOD MODE contains extremely dangerous commands that can wipe your device,
unlock the bootloader (which itself wipes data), or soft-brick the phone if
used incorrectly. Use only on devices you own and understand.

## License

[MIT](LICENSE)
