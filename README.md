# adbgo

Fast terminal power tool for ADB: dual CLI + interactive TUI, wireless ADB, live logcat, interactive file manager, Split APK sideloading & backup, screen recording, live diagnostics dashboard, multi-device batch mode — plus a device login system, GOD MODE, and nuclear options for devices you own.

Built on Linux (Arch/Omarchy first-class) with Go + [huh](https://github.com/charmbracelet/huh) + [lipgloss](https://github.com/charmbracelet/lipgloss). Works on Linux, macOS, and Windows with Go + `android-tools`.

---

## Features

- **Dual-Mode**: Run interactively (TUI) or script via direct CLI commands (`adbgo screenshot`, `adbgo scrcpy`, `adbgo install`).
- **⚡ Live Device Dashboard**: Real-time battery status (level, health, temperature in °C, voltage, charger source), display resolution & DPI, currently active app/activity, and storage table (`/data`, `/system`).
- **Wireless ADB**:
  - `adb tcpip 5555` USB-to-wireless flow (auto-detects phone IP on `wlan0`).
  - **Android 11+ Pairing**: Pair cable-free with 6-digit pairing code (`adb pair ip:port code`).
  - **mDNS Discovery**: Scan local Wi-Fi for wireless debugging devices.
- **APK Power Tools**:
  - Sideload single APKs or Split APK bundles (`.xapk`, `.apks`, `.zip`) via `adb install-multiple`.
  - Extract/backup installed APKs from phone directly to PC.
  - Launch apps, force-stop, clear data/cache, or uninstall.
- **Interactive File Manager**:
  - Browse `/sdcard` with push, pull, mkdir, text preview, and deletion.
  - Use terminal file picker to choose local files to push.
  - **Photo Sync**: 1-click sync of `/sdcard/DCIM/Camera` photos to PC.
- **Screen & scrcpy Power Tools**:
  - **Clean scrcpy Launcher**: SDL/ffmpeg logs run in background (`~/.config/adbgo/scrcpy.log`) so the terminal UI stays pristine.
  - **High-End Profiles**: Battery Saver (turn screen off & stay awake), Low-Latency Wi-Fi mode (1024p, 4Mbps, 60fps), Ultra Gaming (120fps & H.265), Always-on-Top floating window, Direct MP4 record to PC, and Custom Configuration Builder.
  - Screenshot capture pulled automatically to PC as PNG.
  - Screen recording (`screenrecord`) with auto-pull and cleanup upon `Ctrl+C` (gracefully trapped without killing the tool).
- **Live Logcat Viewer**:
  - Filter by package PID, log tag, and priority level (V, D, I, W, E, F).
  - Dump crash buffer (`adb logcat -b crash`).
- **Developer Utilities**:
  - Type text/passwords into phone fields with proper space & character escaping.
  - Clean Demo Mode toggle (sets 12:00 clock, 100% battery, hides notifications for screenshots).
  - Deep Link tester (`am start -a android.intent.action.VIEW -d <url>`).
- **Multi-Device Mode**:
  - Batch wake, lock, reboot, take screenshots, or install APK across multiple connected devices simultaneously.
- **Security & Registration**:
  - Device signup/login system tied to serial + bcrypt password hash.
  - Rename, remove, export, and import registered devices.
  - 30-minute session inactivity timeout.
- **GOD MODE ("Danger Zone")**:
  - Password-protected with strict `YES` + serial confirmation.
  - Interactive root shell with allocated PTY (`adb shell -t su`) or `adb root`.
  - Fastboot bootloader unlock/lock with auto-wait detection.
  - Factory reset, cache wipe, and raw command passthrough.
- **In-App Self Updater**:
  - Checks GitHub Releases, downloads asset for your OS/Arch, and replaces the binary in-place.

---

## Requirements

```bash
# Arch Linux / Omarchy
sudo pacman -S go android-tools scrcpy

# Debian / Ubuntu
sudo apt install golang-go adb scrcpy

# Fedora
sudo dnf install golang android-tools scrcpy

# macOS (Homebrew)
brew install go android-platform-tools scrcpy
```

---

## Install

### Option 1: `go install` (Recommended)
```bash
go install github.com/dev-roba/adbgo@latest
```

### Option 2: Prebuilt Binary
Grab the latest release archive from [GitHub Releases](https://github.com/dev-roba/adbgo/releases), extract, and copy `adbgo` to your `PATH` (e.g. `~/.local/bin/`).

### Option 3: Build from source
```bash
git clone https://github.com/dev-roba/adbgo.git
cd adbgo
go build -o adbgo .
cp adbgo ~/.local/bin/
```

---

## Usage

### Interactive TUI Mode
Run `adbgo` without any arguments:
```bash
adbgo
```
- **First time**: Select **Signup** $\to$ pick your connected device from `adb devices` $\to$ set a password (min 8 characters).
- **Subsequent runs**: Select **Login** $\to$ choose your registered device.

### CLI Direct Mode
Use `adbgo` directly from your shell, terminal scripts, or keybindings:

```bash
# List all connected devices
adbgo devices

# Open live hardware & system dashboard
adbgo dashboard
adbgo dashboard -s <serial>

# Launch scrcpy mirror
adbgo scrcpy

# Capture screenshot to local directory
adbgo screenshot

# Record screen (Ctrl+C to stop, pulls rec-*.mp4 to current directory)
adbgo record

# Install APK or split bundle (.xapk / .apks)
adbgo install app.apk
adbgo install -d update_bundle.xapk

# Stream live logcat filtered by package
adbgo logcat -p com.example.app -l W

# Wireless ADB shortcuts
adbgo wireless connect              # Switch USB device to tcpip 5555 and connect
adbgo wireless pair 192.168.1.50:37281 123456 # Pair Android 11+
adbgo wireless scan                 # Scan local network for mDNS wireless devices
adbgo wireless disconnect 192.168.1.50:5555

# Open interactive shell
adbgo shell

# Check for updates and self-update
adbgo update
```

---

## Configuration & Storage

Registered devices, friendly names, and bcrypt password hashes are stored in:
```
~/.config/adbgo/devices.json
```
Use **Auth Menu $\to$ Export Devices** to backup or transfer registrations between machines.

---

## 🔐 Pentest Lab

adbgo ships recon & security-testing features for **authorized use only** — your own device, or one you have written permission to test. In the TUI: Main Menu → 🛡️ Pentest Lab.

**Device fingerprint** — security posture profile (build, patch age, SELinux, verified boot, encryption, root indicators) with an attack-surface verdict:

```bash
adbgo fingerprint [-s serial] [-json profile.json]
```

**Attack-surface dump** — flags risky exported components (activities / services / receivers / providers), `debuggable` & `allowBackup` flags, and dangerous permissions per app:

```bash
adbgo surface -pkg com.example.app      # single app
adbgo surface -all                       # all user apps, ranked by findings
adbgo surface -all -json report.json    # save as JSON
```

**Lockscreen credential tester** — throttle-aware PIN/password/pattern testing:

```bash
# TUI: Main Menu → ⚠ DANGER ZONE → 🔓 Lockscreen Credential Tester (GOD MODE)

adbgo bruteforce -type pin -min 4 -max 4 -delay 500
adbgo bruteforce -type pin -mask 08??                    # known prefix
adbgo bruteforce -type password -wordlist words.txt     # case & suffix mutations
adbgo bruteforce -type pattern -min 3 -max 5            # valid 3×3 patterns only
adbgo bruteforce -type pin -resume                       # continue a saved session
```

- **Credential oracle** via the framework's `locksettings verify` (Android 10+) — correct / incorrect / throttled, without touching the screen
- **PIN**: frequency-ordered common PINs first, masks like `08??`, configurable charset
- **Password**: wordlists with case + common-suffix mutations, or charset brute-force
- **Pattern**: valid-only generation respecting Android's 3×3 skip rules (320 → 389k candidates by length)
- **Throttle-aware**: exponential backoff (30s → 15 min cap), configurable delay + jitter, hard attempt cap
- **Resumable**: every tried candidate is saved to `~/.config/adbgo/bruteforce/`; Ctrl+C saves and `-resume` continues
- **Guardrails**: authorization confirmation, factory-reset wipe warning, GOD MODE gating in the TUI

## Development & Testing

```bash
# Run unit tests
go test -v ./...

# Run static analysis
go vet ./...

# Check code formatting
gofmt -l .
```

Releases are automatically published via GoReleaser on every git tag (`v*`).

---

## License

[MIT](LICENSE)
