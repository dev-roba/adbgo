package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"adbgo/internal/adb"
	"adbgo/internal/auth"
	"adbgo/internal/config"
	"adbgo/internal/danger"
	"adbgo/internal/ui"

	"github.com/charmbracelet/huh"
)

func main() {
	// Check adb is available
	if _, err := exec.LookPath("adb"); err != nil {
		ui.PrintError("adb not found in PATH. Install android-tools first.")
		os.Exit(1)
	}

	cfg, err := config.Load()
	if err != nil {
		ui.PrintError("Failed to load config: " + err.Error())
		os.Exit(1)
	}

	var session *auth.Session

	for {
		if session == nil || session.IsExpired() {
			if session != nil && session.IsExpired() {
				ui.PrintWarning("Session expired (30 min inactivity). Please login again.")
				ui.Pause()
			}
			session = showAuthMenu(cfg)
			if session == nil {
				ui.PrintSuccess("Goodbye.")
				return
			}
		}

		session.Refresh()
		showMainMenu(cfg, session)
	}
}

func showAuthMenu(cfg *config.Config) *auth.Session {
	ui.ClearScreen()
	fmt.Println(ui.TitleStyle.Render("adbgo — Authentication"))
	fmt.Println()

	var choice string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("What do you want to do?").
				Options(
					huh.NewOption("Login", "login"),
					huh.NewOption("Signup (register new device)", "signup"),
					huh.NewOption("Export devices", "export"),
					huh.NewOption("Import devices", "import"),
					huh.NewOption("Exit", "exit"),
				).
				Value(&choice),
		),
	)

	if err := form.Run(); err != nil {
		return nil
	}

	switch choice {
	case "login":
		return doLogin(cfg)
	case "signup":
		return doSignup(cfg)
	case "export":
		doExport(cfg)
		return showAuthMenu(cfg)
	case "import":
		doImport(cfg)
		return showAuthMenu(cfg)
	default:
		return nil
	}
}

func doSignup(cfg *config.Config) *auth.Session {
	devices, err := adb.ListDevices()
	if err != nil {
		ui.PrintError("Failed to list devices: " + err.Error())
		ui.Pause()
		return nil
	}

	// Filter only online devices
	var online []adb.DeviceInfo
	for _, d := range devices {
		if d.Status == "device" {
			online = append(online, d)
		}
	}

	if len(online) == 0 {
		ui.PrintError("No online devices found. Connect a device and enable USB debugging.")
		ui.Pause()
		return nil
	}

	options := make([]huh.Option[string], len(online))
	for i, d := range online {
		label := d.Serial
		if d.Model != "" {
			label = fmt.Sprintf("%s (%s)", d.Serial, d.Model)
		}
		options[i] = huh.NewOption(label, d.Serial)
	}

	var selectedSerial string
	var name string
	var password string
	var confirm string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select device to register").
				Options(options...).
				Value(&selectedSerial),
			huh.NewInput().
				Title("Friendly name (optional)").
				Placeholder("My Pixel / Work Phone...").
				Value(&name),
			huh.NewInput().
				Title("Set password").
				EchoMode(huh.EchoModePassword).
				Value(&password),
			huh.NewInput().
				Title("Confirm password").
				EchoMode(huh.EchoModePassword).
				Value(&confirm),
		),
	)

	if err := form.Run(); err != nil {
		return nil
	}

	if password != confirm {
		ui.PrintError("Passwords do not match")
		ui.Pause()
		return nil
	}
	if len(password) < 4 {
		ui.PrintError("Password too short (min 4 characters)")
		ui.Pause()
		return nil
	}

	session, err := auth.Signup(cfg, selectedSerial, name, password)
	if err != nil {
		ui.PrintError(err.Error())
		ui.Pause()
		return nil
	}

	ui.PrintSuccess(fmt.Sprintf("Device '%s' registered and logged in!", session.Device.Name))
	ui.Pause()
	return session
}

func doLogin(cfg *config.Config) *auth.Session {
	if len(cfg.Devices) == 0 {
		ui.PrintError("No devices registered yet. Please Signup first.")
		ui.Pause()
		return nil
	}

	// Show only registered devices that are currently connected
	connected := []config.Device{}
	for _, d := range cfg.Devices {
		if adb.IsConnected(d.Serial) {
			connected = append(connected, d)
		}
	}

	if len(connected) == 0 {
		ui.PrintError("None of your registered devices are currently connected.")
		ui.Pause()
		return nil
	}

	options := make([]huh.Option[string], len(connected))
	for i, d := range connected {
		options[i] = huh.NewOption(fmt.Sprintf("%s (%s)", d.Name, d.Serial), d.Serial)
	}

	var selectedSerial string
	var password string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Select device").
				Options(options...).
				Value(&selectedSerial),
			huh.NewInput().
				Title("Password").
				EchoMode(huh.EchoModePassword).
				Value(&password),
		),
	)

	if err := form.Run(); err != nil {
		return nil
	}

	session, err := auth.Login(cfg, selectedSerial, password)
	if err != nil {
		ui.PrintError(err.Error())
		ui.Pause()
		return nil
	}

	ui.PrintSuccess("Logged in successfully!")
	time.Sleep(600 * time.Millisecond)
	return session
}

func showMainMenu(cfg *config.Config, session *auth.Session) {
	for {
		if session.IsExpired() {
			return // back to auth
		}

		ui.PrintBanner(session.GodMode, session.Device.Name, session.Device.Serial)

		var choice string

		options := []huh.Option[string]{
			huh.NewOption("Start scrcpy", "scrcpy"),
			huh.NewOption("Device Control", "device"),
			huh.NewOption("App Management", "apps"),
			huh.NewOption("File & System", "files"),
		}

		if session.GodMode {
			options = append(options,
				huh.NewOption("⚠  DANGER ZONE", "danger"),
				huh.NewOption("Disable GOD MODE", "disable_god"),
			)
		} else {
			options = append(options, huh.NewOption("Enable GOD MODE", "god"))
		}

		options = append(options,
			huh.NewOption("Logout", "logout"),
			huh.NewOption("Exit", "exit"),
		)

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Main Menu").
					Options(options...).
					Value(&choice),
			),
		)

		if err := form.Run(); err != nil {
			return
		}

		session.Refresh()

		switch choice {
		case "scrcpy":
			ui.PrintSuccess("Launching scrcpy...")
			if err := adb.StartScrcpy(session.Device.Serial); err != nil {
				ui.PrintError("scrcpy failed: " + err.Error())
				ui.PrintWarning("Is scrcpy installed? (sudo pacman -S scrcpy)")
			}
			ui.Pause()

		case "device":
			showDeviceControl(session)

		case "apps":
			showAppManagement(session)

		case "files":
			showFileSystem(session)

		case "god":
			enableGodMode(session)

		case "disable_god":
			session.GodMode = false
			ui.PrintSuccess("GOD MODE disabled")
			time.Sleep(700 * time.Millisecond)

		case "danger":
			showDangerZone(session)

		case "logout":
			return

		case "exit":
			ui.PrintSuccess("Goodbye.")
			os.Exit(0)
		}
	}
}

func enableGodMode(session *auth.Session) {
	ui.PrintWarning("You are about to enable GOD MODE.")
	ui.PrintWarning("This unlocks factory reset, bootloader unlock, FRP tools, etc.")
	fmt.Print("Type YES to enable GOD MODE: ")
	var input string
	fmt.Scanln(&input)
	if strings.TrimSpace(input) == "YES" {
		session.GodMode = true
		ui.PrintSuccess("GOD MODE ENABLED")
	} else {
		ui.PrintError("Cancelled")
	}
	time.Sleep(800 * time.Millisecond)
}

func showDeviceControl(session *auth.Session) {
	var choice string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Device Control").
				Options(
					huh.NewOption("Reboot", "reboot"),
					huh.NewOption("Reboot to Recovery", "recovery"),
					huh.NewOption("Reboot to Bootloader", "bootloader"),
					huh.NewOption("Power Off", "poweroff"),
					huh.NewOption("Take Screenshot", "screenshot"),
					huh.NewOption("Back", "back"),
				).
				Value(&choice),
		),
	)
	form.Run()

	serial := session.Device.Serial
	switch choice {
	case "reboot":
		adb.Reboot(serial, "")
		ui.PrintSuccess("Reboot command sent")
	case "recovery":
		adb.Reboot(serial, "recovery")
		ui.PrintSuccess("Rebooting to recovery")
	case "bootloader":
		adb.Reboot(serial, "bootloader")
		ui.PrintSuccess("Rebooting to bootloader")
	case "poweroff":
		adb.Run(serial, "shell", "reboot", "-p")
		ui.PrintSuccess("Power off command sent")
	case "screenshot":
		filename := fmt.Sprintf("screenshot-%s.png", time.Now().Format("20060102-150405"))
		adb.Run(serial, "shell", "screencap", "-p", "/sdcard/"+filename)
		adb.Run(serial, "pull", "/sdcard/"+filename, filename)
		ui.PrintSuccess("Screenshot saved as " + filename)
	}
	if choice != "back" {
		ui.Pause()
	}
}

func showAppManagement(session *auth.Session) {
	var choice string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("App Management").
				Options(
					huh.NewOption("List user packages", "list_user"),
					huh.NewOption("List all packages", "list_all"),
					huh.NewOption("Force-stop app", "force_stop"),
					huh.NewOption("Clear app data", "clear"),
					huh.NewOption("Uninstall app", "uninstall"),
					huh.NewOption("Back", "back"),
				).
				Value(&choice),
		),
	)
	form.Run()

	serial := session.Device.Serial
	switch choice {
	case "list_user":
		out, _ := adb.Run(serial, "shell", "pm", "list", "packages", "-3")
		fmt.Println(out)
	case "list_all":
		out, _ := adb.Run(serial, "shell", "pm", "list", "packages")
		fmt.Println(out)
	case "force_stop", "clear", "uninstall":
		fmt.Print("Enter package name: ")
		var pkg string
		fmt.Scanln(&pkg)
		if pkg == "" {
			return
		}
		switch choice {
		case "force_stop":
			adb.Run(serial, "shell", "am", "force-stop", pkg)
			ui.PrintSuccess("Force-stopped " + pkg)
		case "clear":
			adb.Run(serial, "shell", "pm", "clear", pkg)
			ui.PrintSuccess("Cleared data for " + pkg)
		case "uninstall":
			adb.Run(serial, "uninstall", pkg)
			ui.PrintSuccess("Uninstalled " + pkg)
		}
	}
	if choice != "back" {
		ui.Pause()
	}
}

func showFileSystem(session *auth.Session) {
	var choice string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("File & System").
				Options(
					huh.NewOption("Open interactive shell", "shell"),
					huh.NewOption("Device info", "info"),
					huh.NewOption("Back", "back"),
				).
				Value(&choice),
		),
	)
	form.Run()

	serial := session.Device.Serial
	switch choice {
	case "shell":
		ui.PrintWarning("Starting adb shell. Type 'exit' to return.")
		cmd := exec.Command("adb", "-s", serial, "shell")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
	case "info":
		out, _ := adb.Run(serial, "shell", "getprop")
		// Print only interesting props
		interesting := []string{"ro.product.model", "ro.build.version.release", "ro.serialno", "ro.product.manufacturer"}
		for _, line := range strings.Split(out, "\n") {
			for _, key := range interesting {
				if strings.Contains(line, key) {
					fmt.Println(line)
				}
			}
		}
		ui.Pause()
	}
}

func showDangerZone(session *auth.Session) {
	var choice string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(ui.WarningStyle.Render("DANGER ZONE — NUCLEAR OPTIONS")).
				Options(
					huh.NewOption("Factory Reset / Wipe Data", "factory"),
					huh.NewOption("Wipe Cache", "cache"),
					huh.NewOption("Unlock Bootloader", "unlock"),
					huh.NewOption("Lock Bootloader", "lock"),
					huh.NewOption("Remove Screen Lock", "lockscreen"),
					huh.NewOption("Attempt Disable FRP", "frp"),
					huh.NewOption("Format /data", "format_data"),
					huh.NewOption("Interactive Root Shell", "root"),
					huh.NewOption("Raw adb command", "raw_adb"),
					huh.NewOption("Raw fastboot command", "raw_fastboot"),
					huh.NewOption("Back", "back"),
				).
				Value(&choice),
		),
	)
	form.Run()

	serial := session.Device.Serial
	var err error

	switch choice {
	case "factory":
		err = danger.FactoryReset(serial)
	case "cache":
		err = danger.WipeCache(serial)
	case "unlock":
		err = danger.UnlockBootloader(serial)
	case "lock":
		err = danger.LockBootloader(serial)
	case "lockscreen":
		err = danger.RemoveScreenLock(serial)
	case "frp":
		err = danger.DisableFRP(serial)
	case "format_data":
		err = danger.FormatData(serial)
	case "root":
		err = danger.FullRootShell(serial)
	case "raw_adb":
		err = danger.RawADB(serial)
	case "raw_fastboot":
		err = danger.RawFastboot(serial)
	}

	if err != nil {
		ui.PrintError(err.Error())
	}
	if choice != "back" {
		ui.Pause()
	}
}

func doExport(cfg *config.Config) {
	path, _ := config.ConfigPath()
	ui.PrintSuccess("Config already at: " + path)
	ui.PrintSuccess("Just copy ~/.config/adbgo/devices.json to the other machine.")
	ui.Pause()
}

func doImport(cfg *config.Config) {
	ui.PrintWarning("Place your devices.json into ~/.config/adbgo/ then restart adbgo.")
	ui.Pause()
}
