# adbgo

Fast terminal power tool for ADB with login system, GOD MODE, and nuclear options.

Built for Omarchy / Arch Linux (works on any Linux with Go + android-tools).

## Features

- **Login / Signup** system tied to device serial + password
- Custom friendly names for devices
- 30-minute session timeout
- Colorful UI (green normal / red GOD MODE)
- scrcpy integration
- Device control (reboot, recovery, bootloader, screenshots...)
- App management
- **GOD MODE** → unlocks full Danger Zone
- Nuclear options: factory reset, bootloader unlock/lock, remove screen lock, FRP attempts, raw adb/fastboot, etc.
- Every dangerous action requires typing `YES` (and serial for the worst ones)

## Requirements

```bash
sudo pacman -S go android-tools scrcpy
```

## Build

```bash
cd adbgo
go build -o adbgo .
```

## Install (optional)

```bash
cp adbgo ~/.local/bin/   # or any directory in your PATH
```

## Usage

```bash
./adbgo
```

First time → Signup → select your device from `adb devices` → set password.

Next times → Login with the registered device.

## Config location

`~/.config/adbgo/devices.json`

You can copy this file to move your registered devices to another machine.

## Warning

GOD MODE contains extremely dangerous commands that can wipe your device, unlock the bootloader (usually wipes data), or soft-brick the phone if used incorrectly. Use only on devices you own and understand.
