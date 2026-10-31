package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"github.com/dev-roba/adbgo/internal/adb"
	"github.com/dev-roba/adbgo/internal/auth"
	"github.com/dev-roba/adbgo/internal/cli"
	"github.com/dev-roba/adbgo/internal/config"
	"github.com/dev-roba/adbgo/internal/danger"
	"github.com/dev-roba/adbgo/internal/share"
	"github.com/dev-roba/adbgo/internal/ui"
	"github.com/dev-roba/adbgo/internal/update"

	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// version is injected at build time by GoReleaser (-X main.version=...);
// "dev" means it was built from source without version info.
var version = "dev"

func main() {
	// Check adb is available
	if _, err := exec.LookPath("adb"); err != nil {
		ui.PrintError("adb not found in PATH. Install android-tools first.")
		os.Exit(1)
	}

	// Dispatch CLI subcommands if arguments were passed
	if cli.HandleCLI(version) {
		return
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
					huh.NewOption("📶 Wireless ADB (connect over Wi-Fi / pair)", "wireless"),
					huh.NewOption("Device Management (rename / remove)", "manage"),
					huh.NewOption("Export devices", "export"),
					huh.NewOption("Import devices", "import"),
					huh.NewOption("🔄 Check for Updates", "update"),
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
	case "wireless":
		wirelessFlow("")
		return showAuthMenu(cfg)
	case "manage":
		showDeviceManagement(cfg)
		return showAuthMenu(cfg)
	case "export":
		doExport(cfg)
		return showAuthMenu(cfg)
	case "import":
		doImport(cfg)
		return showAuthMenu(cfg)
	case "update":
		checkForUpdates()
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
				Title("Set password (min 8 characters)").
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

	connected := []config.Device{}
	for _, d := range cfg.Devices {
		if adb.IsConnected(d.Serial) {
			connected = append(connected, d)
		}
	}

	if len(connected) == 0 {
		ui.PrintError("None of your registered devices are currently connected.")
		ui.PrintInfo("Tip: register the wireless serial (ip:5555) too, so you can login over Wi-Fi.")
		ui.Pause()
		return nil
	}

	var selectedSerial string
	if len(connected) == 1 {
		selectedSerial = connected[0].Serial
	} else {
		options := make([]huh.Option[string], len(connected))
		for i, d := range connected {
			options[i] = huh.NewOption(fmt.Sprintf("%s (%s)", d.Name, d.Serial), d.Serial)
		}

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Select connected device").
					Options(options...).
					Value(&selectedSerial),
			),
		)

		if err := form.Run(); err != nil || selectedSerial == "" {
			return nil
		}
	}

	session, err := auth.LoginWithoutPassword(cfg, selectedSerial)
	if err != nil {
		ui.PrintError(err.Error())
		ui.Pause()
		return nil
	}

	ui.PrintSuccess(fmt.Sprintf("Logged in as '%s' (%s)!", session.Device.Name, session.Device.Serial))
	time.Sleep(500 * time.Millisecond)
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
			huh.NewOption("🔓 Unlock Phone (wake, swipe & enter PIN)", "unlock_phone"),
			huh.NewOption("🔒 Lock Phone (turn screen off)", "lock_phone"),
			huh.NewOption("⚡ Device Dashboard (battery, specs, storage)", "dashboard"),
			huh.NewOption("📱 Start scrcpy (mirror & control)", "scrcpy"),
			huh.NewOption("📶 Wireless ADB (connect Wi-Fi, pairing, mDNS)", "wireless"),
			huh.NewOption("🎮 Device Control (screen, reboot, typing)", "device"),
			huh.NewOption("📦 App Management (install, extract, freeze)", "apps"),
			huh.NewOption("📂 File & System (files, photo sync, logcat)", "files"),
			huh.NewOption("👥 Multi-Device Mode", "multi"),
			huh.NewOption("🛡️ Pentest Lab (fingerprint & attack surface)", "pentest"),
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
			huh.NewOption("🔄 Check for Updates", "update"),
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
		case "unlock_phone":
			unlockDeviceFlow(cfg, session)

		case "lock_phone":
			ui.PrintInfo("Locking device (switching screen off)...")
			if err := adb.LockScreen(session.Device.Serial); err != nil {
				ui.PrintError("Lock failed: " + err.Error())
			} else {
				ui.PrintSuccess("Screen turned off (phone locked)!")
			}
			time.Sleep(700 * time.Millisecond)

		case "dashboard":
			showDashboard(session)

		case "scrcpy":
			showScrcpyMenu(session)

		case "wireless":
			wirelessFlow(session.Device.Serial)

		case "device":
			showDeviceControl(cfg, session)

		case "apps":
			showAppManagement(session)

		case "files":
			showFileSystem(session)

		case "multi":
			showMultiDevice(session)

		case "pentest":
			showPentestLab(session)

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

		case "update":
			checkForUpdates()

		case "exit":
			ui.PrintSuccess("Goodbye.")
			os.Exit(0)
		}
	}
}

func showDashboard(session *auth.Session) {
	serial := session.Device.Serial
	ui.ClearScreen()
	fmt.Println(ui.TitleStyle.Render("⚡ Live Device Dashboard — " + session.Device.Name))
	fmt.Println()

	// Battery
	batt, err := adb.GetBatteryInfo(serial)
	if err == nil {
		fmt.Printf("%s  %d%%  (%s, %.1f°C, %.2fV, Source: %s)\n",
			ui.Highlight.Render("Battery:    "),
			batt.Level,
			batt.Status,
			batt.Temperature,
			batt.Voltage,
			batt.PowerSource,
		)
	}

	// Display
	size, density, _ := adb.GetDisplaySpecs(serial)
	if size != "" {
		fmt.Printf("%s  %s @ %s dpi\n", ui.Highlight.Render("Display:    "), size, density)
	}

	// Focused App
	focus := adb.GetCurrentFocus(serial)
	fmt.Printf("%s  %s\n", ui.Highlight.Render("Active App: "), focus)

	// OS Info
	rel, _ := adb.Run(serial, "shell", "getprop", "ro.build.version.release")
	sdk, _ := adb.Run(serial, "shell", "getprop", "ro.build.version.sdk")
	patch, _ := adb.Run(serial, "shell", "getprop", "ro.build.version.security_patch")
	fmt.Printf("%s  Android %s (SDK %s) — Security patch: %s\n",
		ui.Highlight.Render("Android OS: "),
		strings.TrimSpace(rel),
		strings.TrimSpace(sdk),
		strings.TrimSpace(patch),
	)

	// Storage
	storage, err := adb.GetStorageInfo(serial)
	if err == nil && len(storage) > 0 {
		fmt.Println("\n" + ui.Highlight.Render("Storage Partitions:"))
		fmt.Printf("  %-12s %-10s %-10s %-10s %s\n", "MOUNT", "TOTAL", "USED", "FREE", "USE%")
		for _, s := range storage {
			fmt.Printf("  %-12s %-10s %-10s %-10s %s\n", s.MountedOn, s.Size, s.Used, s.Avail, s.UsePercent)
		}
	}
	fmt.Println()
	ui.Pause()
}

func showScrcpyMenu(session *auth.Session) {
	serial := session.Device.Serial

	var profileChoice string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("📱 scrcpy Power Menu — "+session.Device.Name).
				Options(
					huh.NewOption("🚀 Standard Mirror (default settings)", "standard"),
					huh.NewOption("🔋 Battery Saver (Turn phone screen off & stay awake)", "screen_off"),
					huh.NewOption("⚡ Low-Latency Wi-Fi Mode (1024p, 4M bitrate, 60 FPS)", "wifi"),
					huh.NewOption("🎮 Ultra Gaming Mode (120 FPS & H.265 codec)", "gaming"),
					huh.NewOption("📌 Always-On-Top Floating Window", "always_on_top"),
					huh.NewOption("🎥 Direct Screen Record to PC (.mp4)", "record"),
					huh.NewOption("🔊 Audio Forwarding (Duplicate to PC & Phone)", "audio_dup"),
					huh.NewOption("⌨️ HID Keyboard & Mouse (OTG hardware emulation)", "hid"),
					huh.NewOption("🛠️ Custom Configuration Builder", "custom"),
					huh.NewOption("Back", "back"),
				).
				Value(&profileChoice),
		),
	)
	if err := form.Run(); err != nil || profileChoice == "back" || profileChoice == "" {
		return
	}

	var profile adb.ScrcpyProfile
	switch profileChoice {
	case "standard":
		profile = adb.ScrcpyProfile{Name: "Standard"}

	case "screen_off":
		profile = adb.ScrcpyProfile{
			Name:          "Battery Saver (Screen Off)",
			TurnScreenOff: true,
			StayAwake:     true,
		}

	case "wifi":
		profile = adb.ScrcpyProfile{
			Name:     "Low-Latency Wi-Fi",
			MaxSize:  1024,
			BitRateM: 4,
			MaxFPS:   60,
		}

	case "gaming":
		profile = adb.ScrcpyProfile{
			Name:      "Ultra Gaming (120 FPS)",
			MaxFPS:    120,
			BitRateM:  16,
			Codec:     "h265",
			StayAwake: true,
		}

	case "always_on_top":
		profile = adb.ScrcpyProfile{
			Name:        "Always on Top",
			AlwaysOnTop: true,
		}

	case "record":
		profile = adb.ScrcpyProfile{
			Name:       "Direct PC Record",
			RecordToPC: true,
		}

	case "audio_dup":
		profile = adb.ScrcpyProfile{
			Name:     "Audio Duplicate",
			AudioDup: true,
		}

	case "hid":
		profile = adb.ScrcpyProfile{
			Name:      "HID Keyboard & Mouse",
			ExtraArgs: []string{"--keyboard=uhid", "--mouse=uhid"},
		}

	case "custom":
		profile = buildCustomScrcpyProfile()
		if profile.Name == "" {
			return
		}
	}

	ui.ClearScreen()
	banner := ui.NormalBanner.Render(fmt.Sprintf("📱 scrcpy active: %s", profile.Name))
	fmt.Println(banner)
	fmt.Printf("%s %s (%s)\n", ui.DimStyle.Render("Target:"), session.Device.Name, serial)
	fmt.Println(ui.DimStyle.Render("Logs redirected to ~/.config/adbgo/scrcpy.log (Terminal stays clean)"))
	fmt.Println(ui.WarningStyle.Render("Close the scrcpy window to return to adbgo."))
	fmt.Println()

	recFile, err := adb.LaunchScrcpyClean(serial, profile)
	if err != nil {
		ui.PrintError(err.Error())
		ui.PrintWarning("Is scrcpy installed? (pacman -S scrcpy / apt install scrcpy)")
	} else {
		ui.PrintSuccess("scrcpy session finished cleanly.")
		if recFile != "" {
			ui.PrintSuccess("Recording saved: " + recFile)
		}
	}
	ui.Pause()
}

func buildCustomScrcpyProfile() adb.ScrcpyProfile {
	var turnScreenOff bool
	var stayAwake bool
	var alwaysOnTop bool
	var borderless bool
	var audioMode string
	var resolution int
	var fps int

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title("Turn device screen off while mirroring?").
				Value(&turnScreenOff),
			huh.NewConfirm().
				Title("Keep device awake while plugged in?").
				Value(&stayAwake),
			huh.NewConfirm().
				Title("Keep window always on top?").
				Value(&alwaysOnTop),
			huh.NewConfirm().
				Title("Borderless window?").
				Value(&borderless),
			huh.NewSelect[int]().
				Title("Max resolution limit").
				Options(
					huh.NewOption("Native (Unlimited)", 0),
					huh.NewOption("1080p (1920x1080)", 1920),
					huh.NewOption("720p (1280x720) — Recommended for Wi-Fi", 1280),
					huh.NewOption("480p (854x480) — Ultra fast", 854),
				).
				Value(&resolution),
			huh.NewSelect[int]().
				Title("Max Framerate").
				Options(
					huh.NewOption("Default", 0),
					huh.NewOption("60 FPS", 60),
					huh.NewOption("90 FPS", 90),
					huh.NewOption("120 FPS", 120),
				).
				Value(&fps),
			huh.NewSelect[string]().
				Title("Audio Routing").
				Options(
					huh.NewOption("PC only (Default)", "pc"),
					huh.NewOption("Both PC and Phone (Duplicate)", "dup"),
					huh.NewOption("Disabled (No audio)", "none"),
				).
				Value(&audioMode),
		),
	)

	if err := form.Run(); err != nil {
		return adb.ScrcpyProfile{}
	}

	return adb.ScrcpyProfile{
		Name:          "Custom Config",
		TurnScreenOff: turnScreenOff,
		StayAwake:     stayAwake,
		AlwaysOnTop:   alwaysOnTop,
		Borderless:    borderless,
		MaxSize:       resolution,
		MaxFPS:        fps,
		NoAudio:       audioMode == "none",
		AudioDup:      audioMode == "dup",
	}
}

func checkForUpdates() {
	fmt.Println()
	ui.PrintInfo("Current version: " + version)
	ui.PrintInfo("Checking GitHub for updates...")

	latest, err := update.Latest()
	if err != nil {
		ui.PrintError("Could not check for updates: " + err.Error())
		ui.PrintWarning("Are you online? You can also update manually: go install github.com/dev-roba/adbgo@latest")
		ui.Pause()
		return
	}
	ui.PrintInfo("Latest release:  " + latest)

	if version != "dev" && !update.IsNewer(latest, version) {
		ui.PrintSuccess("adbgo is up to date.")
		ui.Pause()
		return
	}
	if version == "dev" {
		ui.PrintWarning("You are running a dev build (no version info) — updating is safe but may not be newer.")
	}

	var install bool
	form := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title(fmt.Sprintf("Download and install %s?", latest)).
			Value(&install),
	))
	if err := form.Run(); err != nil || !install {
		return
	}

	ui.PrintInfo("Downloading and installing " + latest + "...")
	if err := update.Install(latest); err != nil {
		ui.PrintError("Update failed: " + err.Error())
		ui.PrintInfo("Fallback: go install github.com/dev-roba/adbgo@latest")
		ui.Pause()
		return
	}
	ui.PrintSuccess("Updated to " + latest + "!")
	ui.PrintWarning("Restart adbgo to run the new version.")
	ui.Pause()
}

func enableGodMode(session *auth.Session) {
	ui.PrintWarning("You are about to enable GOD MODE.")
	ui.PrintWarning("This unlocks factory reset, bootloader unlock, FRP tools, etc.")

	for attempts := 3; attempts > 0; attempts-- {
		fmt.Printf("Enter your password to enable GOD MODE (%d attempt(s) left): ", attempts)
		pw := ui.ReadLine()
		if pw == "" {
			ui.PrintError("Cancelled")
			return
		}
		if auth.CheckPassword(session.Device.Password, pw) {
			session.GodMode = true
			ui.PrintSuccess("GOD MODE ENABLED")
			time.Sleep(800 * time.Millisecond)
			return
		}
		ui.PrintError("Incorrect password")
	}
	ui.PrintError("Too many failed attempts. GOD MODE not enabled.")
	time.Sleep(1 * time.Second)
}

func unlockDeviceFlow(cfg *config.Config, session *auth.Session) {
	serial := session.Device.Serial
	pw := session.Device.UnlockPassword

	if pw == "" {
		ui.PrintWarning("No lockscreen PIN/password stored for " + session.Device.Name)
		fmt.Print("Enter your phone's lockscreen password or PIN: ")
		pw = ui.ReadLine()
		if pw == "" {
			return
		}
		session.Device.UnlockPassword = pw
		cfg.Save()
		ui.PrintSuccess("Saved unlock PIN for future 1-click unlocks!")
	}

	ui.PrintInfo("Unlocking phone (waking screen, swiping up, typing PIN)...")
	if err := adb.UnlockWithPassword(serial, pw); err != nil {
		ui.PrintError("Unlock failed: " + err.Error())
	} else {
		ui.PrintSuccess("Phone unlocked!")
	}
	time.Sleep(800 * time.Millisecond)
}

func showDeviceControl(cfg *config.Config, session *auth.Session) {
	var choice string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Device Control").
				Options(
					huh.NewOption("Unlock with PIN/Password (wake, swipe & enter PIN)", "unlock_pw"),
					huh.NewOption("Lock Screen (turn screen off)", "lock"),
					huh.NewOption("Take Screenshot", "screenshot"),
					huh.NewOption("Record Screen (Ctrl+C to stop & auto-pull)", "record"),
					huh.NewOption("Wake Screen", "wake"),
					huh.NewOption("Send Text Input (type onto phone)", "type"),
					huh.NewOption("Toggle Clean Demo Mode (100% battery, no icons)", "demo"),
					huh.NewOption("Reboot", "reboot"),
					huh.NewOption("Reboot to Recovery", "recovery"),
					huh.NewOption("Reboot to Bootloader", "bootloader"),
					huh.NewOption("Power Off", "poweroff"),
					huh.NewOption("Back", "back"),
				).
				Value(&choice),
		),
	)
	form.Run()

	serial := session.Device.Serial
	switch choice {
	case "unlock_pw":
		unlockDeviceFlow(cfg, session)
	case "screenshot":
		filename, err := adb.Screenshot(serial)
		if err != nil {
			ui.PrintError("Screenshot failed: " + err.Error())
		} else {
			ui.PrintSuccess("Screenshot saved as " + filename)
		}
	case "record":
		local, err := adb.RecordScreen(serial)
		if err != nil {
			ui.PrintError(err.Error())
		} else {
			ui.PrintSuccess("Recording saved as " + local)
		}
	case "wake":
		if err := adb.WakeScreen(serial); err != nil {
			ui.PrintError(err.Error())
		} else {
			ui.PrintSuccess("Screen woken")
		}
	case "lock":
		if err := adb.LockScreen(serial); err != nil {
			ui.PrintError(err.Error())
		} else {
			ui.PrintSuccess("Screen locked")
		}
	case "type":
		fmt.Print("Enter text to type into active field: ")
		text := ui.ReadLine()
		if text != "" {
			if err := adb.SendInputText(serial, text); err != nil {
				ui.PrintError(err.Error())
			} else {
				ui.PrintSuccess("Text sent to device!")
			}
		}
	case "demo":
		var demoAction string
		dForm := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("System UI Demo Mode").
				Options(
					huh.NewOption("Enable (Clean status bar: 12:00, 100% battery)", "on"),
					huh.NewOption("Disable (Restore normal status bar)", "off"),
				).Value(&demoAction),
		))
		dForm.Run()
		if demoAction == "on" {
			if err := adb.EnableDemoMode(serial); err != nil {
				ui.PrintError(err.Error())
			} else {
				ui.PrintSuccess("Demo mode enabled!")
			}
		} else if demoAction == "off" {
			if err := adb.DisableDemoMode(serial); err != nil {
				ui.PrintError(err.Error())
			} else {
				ui.PrintSuccess("Demo mode disabled!")
			}
		}
	case "reboot":
		if err := adb.Reboot(serial, ""); err != nil {
			ui.PrintError(err.Error())
		} else {
			ui.PrintSuccess("Reboot command sent")
		}
	case "recovery":
		if err := adb.Reboot(serial, "recovery"); err != nil {
			ui.PrintError(err.Error())
		} else {
			ui.PrintSuccess("Rebooting to recovery")
		}
	case "bootloader":
		if err := adb.Reboot(serial, "bootloader"); err != nil {
			ui.PrintError(err.Error())
		} else {
			ui.PrintSuccess("Rebooting to bootloader")
		}
	case "poweroff":
		if _, err := adb.Run(serial, "shell", "reboot", "-p"); err != nil {
			ui.PrintError(err.Error())
		} else {
			ui.PrintSuccess("Power off command sent")
		}
	}
	if choice != "back" && choice != "" {
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
					huh.NewOption("Install APK or Split Bundle (.apk, .xapk, .apks)", "install"),
					huh.NewOption("Extract / Backup APK to PC", "extract"),
					huh.NewOption("📷 Extract Photos (copy or cut from phone)", "photos"),
					huh.NewOption("Launch App (search by keyword)", "launch"),
					huh.NewOption("Force-Stop App", "force_stop"),
					huh.NewOption("Clear App Data", "clear"),
					huh.NewOption("Uninstall App", "uninstall"),
					huh.NewOption("List 3rd-Party User Packages", "list_user"),
					huh.NewOption("List All Packages", "list_all"),
					huh.NewOption("Back", "back"),
				).
				Value(&choice),
		),
	)
	form.Run()

	serial := session.Device.Serial
	switch choice {
	case "install":
		installPackageFlow(serial)

	case "extract":
		fmt.Print("Enter package name to extract: ")
		pkg := ui.ReadLine()
		if pkg != "" {
			ui.PrintInfo("Extracting APK(s) for " + pkg + "...")
			files, err := adb.ExtractAPK(serial, pkg, ".")
			if err != nil {
				ui.PrintError(err.Error())
			} else {
				ui.PrintSuccess(fmt.Sprintf("Extracted %d file(s): %s", len(files), strings.Join(files, ", ")))
			}
		}

	case "photos":
		extractPhotosFlow(serial)

	case "launch":
		launchAppFlow(serial)

	case "list_user":
		pkgs, err := adb.UserPackages(serial)
		if err != nil {
			ui.PrintError(err.Error())
		} else {
			fmt.Printf("Total %d user packages:\n", len(pkgs))
			for _, p := range pkgs {
				fmt.Println("  • " + p)
			}
		}

	case "list_all":
		pkgs, err := adb.AllPackages(serial)
		if err != nil {
			ui.PrintError(err.Error())
		} else {
			fmt.Printf("Total %d packages:\n", len(pkgs))
			for _, p := range pkgs {
				fmt.Println("  • " + p)
			}
		}

	case "force_stop", "clear", "uninstall":
		fmt.Print("Enter package name: ")
		pkg := ui.ReadLine()
		if pkg == "" {
			return
		}
		var err error
		switch choice {
		case "force_stop":
			err = adb.ForceStopApp(serial, pkg)
			if err == nil {
				ui.PrintSuccess("Force-stopped " + pkg)
			}
		case "clear":
			err = adb.ClearAppData(serial, pkg)
			if err == nil {
				ui.PrintSuccess("Cleared data for " + pkg)
			}
		case "uninstall":
			err = adb.UninstallApp(serial, pkg)
			if err == nil {
				ui.PrintSuccess("Uninstalled " + pkg)
			}
		}
		if err != nil {
			ui.PrintError(err.Error())
		}
	}
	if choice != "back" && choice != "" {
		ui.Pause()
	}
}

func installPackageFlow(serial string) {
	var pkgPath string
	var downgrade bool

	picker := huh.NewFilePicker().
		Title("Choose APK or Bundle (.apk, .xapk, .apks, .zip)").
		AllowedTypes([]string{".apk", ".apkm", ".apks", ".xapk", ".zip"}).
		Value(&pkgPath)

	form := huh.NewForm(
		huh.NewGroup(picker),
		huh.NewGroup(
			huh.NewConfirm().
				Title("Allow downgrade?").
				Value(&downgrade),
		).WithHide(true),
	)

	if err := form.Run(); err != nil {
		return
	}

	if strings.TrimSpace(pkgPath) == "" {
		fmt.Print("Enter path to file: ")
		pkgPath = ui.ReadLine()
		if pkgPath == "" {
			return
		}
	}

	ui.PrintInfo("Installing " + pkgPath + "...")
	if err := adb.InstallPackage(serial, pkgPath, downgrade); err != nil {
		ui.PrintError("Install failed: " + err.Error())
	} else {
		ui.PrintSuccess("Installed successfully: " + pkgPath)
	}
}

func showFileSystem(session *auth.Session) {
	var choice string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("File & System").
				Options(
					huh.NewOption("Browse device files (push / pull / preview)", "browse"),
					huh.NewOption("Sync Camera Photos to PC", "sync_photos"),
					huh.NewOption("Live Logcat (package/priority/crash buffer)", "logcat"),
					huh.NewOption("Wireless ADB (tcpip, pairing, mDNS)", "wireless"),
					huh.NewOption("Open interactive shell", "shell"),
					huh.NewOption("Open Deep Link / URL intent", "deeplink"),
					huh.NewOption("Device Info (hardware & props)", "info"),
					huh.NewOption("Back", "back"),
				).
				Value(&choice),
		),
	)
	form.Run()

	serial := session.Device.Serial
	switch choice {
	case "browse":
		browseFlow(serial)
	case "sync_photos":
		ui.PrintInfo("Pulling /sdcard/DCIM/Camera to local folder...")
		dest, err := adb.SyncPhotos(serial, "")
		if err != nil {
			ui.PrintError(err.Error())
		} else {
			ui.PrintSuccess("Synced photos to " + dest)
		}
	case "shell":
		ui.PrintWarning("Starting adb shell. Type 'exit' to return.")
		if err := adb.RunStreaming(serial, "shell"); err != nil {
			ui.PrintError("Shell ended with error: " + err.Error())
		}
	case "deeplink":
		fmt.Print("Enter URL or URI scheme (e.g. https://example.com or myapp://open): ")
		url := ui.ReadLine()
		if url != "" {
			if err := adb.OpenDeepLink(serial, url); err != nil {
				ui.PrintError(err.Error())
			} else {
				ui.PrintSuccess("Deep link opened on device!")
			}
		}
	case "logcat":
		logcatFlow(serial)
	case "wireless":
		wirelessFlow(serial)
	case "info":
		showDeviceInfo(serial)
	}
	if choice != "back" && choice != "" {
		ui.Pause()
	}
}

func logcatFlow(serial string) {
	var mode string
	mForm := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Logcat View Mode").
			Options(
				huh.NewOption("Live Streaming", "stream"),
				huh.NewOption("View Recent Crash Buffer", "crash"),
			).Value(&mode),
	))
	mForm.Run()

	if mode == "crash" {
		ui.PrintInfo("Querying crash buffer...")
		_ = adb.RunLogcat(serial, adb.LogcatFilter{DumpCrash: true})
		return
	}

	var pkg string
	var priority string
	var tag string
	var clearFirst bool

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Filter by package (blank = all logs)").
				Placeholder("com.example.app").
				Value(&pkg),
			huh.NewInput().
				Title("Filter by tag (blank = all tags)").
				Placeholder("ActivityManager").
				Value(&tag),
			huh.NewSelect[string]().
				Title("Minimum priority").
				Options(
					huh.NewOption("All", ""),
					huh.NewOption("Verbose (V)", "V"),
					huh.NewOption("Debug (D)", "D"),
					huh.NewOption("Info (I)", "I"),
					huh.NewOption("Warning (W)", "W"),
					huh.NewOption("Error (E)", "E"),
					huh.NewOption("Fatal (F)", "F"),
				).
				Value(&priority),
			huh.NewConfirm().
				Title("Clear the logcat buffer first?").
				Value(&clearFirst),
		),
	)

	if err := form.Run(); err != nil {
		return
	}

	if err := adb.RunLogcat(serial, adb.LogcatFilter{
		Package:    strings.TrimSpace(pkg),
		Tag:        strings.TrimSpace(tag),
		Priority:   priority,
		ClearFirst: clearFirst,
	}); err != nil {
		ui.PrintError("Logcat stopped with error: " + err.Error())
	}
	fmt.Println()
}

func browseFlow(serial string) {
	path := "/sdcard"

	for {
		entries, err := adb.ListRemoteDir(serial, path)
		if err != nil {
			ui.PrintError(err.Error())
			ui.Pause()
			return
		}

		var action string
		entryOptions := make([]huh.Option[string], 0, len(entries)+4)
		entryOptions = append(entryOptions, huh.NewOption("📁 ../ (go up)", "up"))
		entryOptions = append(entryOptions, huh.NewOption("⬆ Push file/folder here", "push"))
		entryOptions = append(entryOptions, huh.NewOption("➕ New folder", "mkdir"))
		entryOptions = append(entryOptions, huh.NewOption("↩ Exit file manager", "exit"))

		for _, e := range entries {
			label := "📄 " + e.Name
			if e.IsDir {
				label = "📁 " + e.Name + "/"
			}
			entryOptions = append(entryOptions, huh.NewOption(label, e.Name))
		}

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Browse: " + path).
					Options(entryOptions...).
					Value(&action),
			),
		)
		if err := form.Run(); err != nil {
			return
		}

		switch action {
		case "exit":
			return
		case "up":
			if path != "/" {
				path = parentDir(path)
			}
			continue
		case "push":
			var local string
			picker := huh.NewFilePicker().Title("Select local file to push").Value(&local)
			if err := huh.NewForm(huh.NewGroup(picker)).Run(); err != nil || local == "" {
				fmt.Print("Or type local path manually: ")
				local = ui.ReadLine()
				if local == "" {
					continue
				}
			}
			ui.PrintInfo("Pushing " + local + " → " + path)
			if err := adb.Push(serial, local, path); err != nil {
				ui.PrintError(err.Error())
			} else {
				ui.PrintSuccess("Pushed " + local)
			}
			ui.Pause()

		case "mkdir":
			fmt.Print("Folder name: ")
			name := ui.ReadLine()
			if name == "" {
				continue
			}
			if err := adb.RemoteMkdir(serial, path+"/"+name); err != nil {
				ui.PrintError(err.Error())
			} else {
				ui.PrintSuccess("Created " + name)
			}
			ui.Pause()

		default:
			entryPath := path + "/" + action
			entry := findEntry(entries, action)
			if entry == nil {
				continue
			}
			if entry.IsDir {
				path = entryPath
				continue
			}

			var fileAction string
			fileForm := huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title(entryPath).
						Options(
							huh.NewOption("⬇ Pull (download to PC)", "pull"),
							huh.NewOption("👁 Preview (view text in terminal)", "preview"),
							huh.NewOption("🗑 Delete (rm -rf)", "delete"),
							huh.NewOption("Cancel", "cancel"),
						).
						Value(&fileAction),
				),
			)
			fileForm.Run()

			switch fileAction {
			case "pull":
				local, err := adb.Pull(serial, entryPath)
				if err != nil {
					ui.PrintError(err.Error())
				} else {
					ui.PrintSuccess("Pulled to " + local)
				}
			case "preview":
				content, err := adb.ReadRemoteFile(serial, entryPath, 40)
				if err != nil {
					ui.PrintError(err.Error())
				} else {
					fmt.Println("\n" + ui.TitleStyle.Render("Preview: "+entryPath))
					fmt.Println(content)
				}
			case "delete":
				if danger.ConfirmYES("DELETE " + entryPath) {
					if err := adb.RemoteDelete(serial, entryPath); err != nil {
						ui.PrintError(err.Error())
					} else {
						ui.PrintSuccess("Deleted " + entryPath)
					}
				} else {
					ui.PrintError("Cancelled")
				}
			}
			ui.Pause()
		}
	}
}

func findEntry(entries []adb.FileEntry, name string) *adb.FileEntry {
	for i := range entries {
		if entries[i].Name == name {
			return &entries[i]
		}
	}
	return nil
}

func parentDir(path string) string {
	path = strings.TrimSuffix(path, "/")
	idx := strings.LastIndex(path, "/")
	if idx <= 0 {
		return "/"
	}
	return path[:idx]
}

func wirelessFlow(serial string) {
	var choice string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Wireless ADB").
				Options(
					huh.NewOption("Enable tcpip 5555 and connect USB device", "connect_tcpip"),
					huh.NewOption("Pair with Android 11+ pairing code", "pair"),
					huh.NewOption("Connect directly to IP:Port", "direct"),
					huh.NewOption("Scan local network for mDNS wireless devices", "scan"),
					huh.NewOption("🔗 Share link / QR (LAN) — phone connects by opening a link", "share_lan"),
					huh.NewOption("🌍 Share link / QR (WAN) — same, over the internet via tunnel", "share_wan"),
					huh.NewOption("Disconnect a wireless device", "disconnect"),
					huh.NewOption("Back", "back"),
				).
				Value(&choice),
		),
	)
	form.Run()

	switch choice {
	case "connect_tcpip":
		target := serial
		if target == "" {
			devices, _ := adb.ListDevices()
			var usbDevices []adb.DeviceInfo
			for _, d := range devices {
				if !strings.Contains(d.Serial, ":") && d.Status == "device" {
					usbDevices = append(usbDevices, d)
				}
			}
			if len(usbDevices) == 0 {
				ui.PrintError("No online USB devices found. Connect via USB cable first.")
				break
			}
			opts := make([]huh.Option[string], len(usbDevices))
			for i, d := range usbDevices {
				label := d.Serial
				if d.Model != "" {
					label = fmt.Sprintf("%s (%s)", d.Serial, d.Model)
				}
				opts[i] = huh.NewOption(label, d.Serial)
			}
			selForm := huh.NewForm(huh.NewGroup(
				huh.NewSelect[string]().Title("Select USB device to switch to Wi-Fi").Options(opts...).Value(&target),
			))
			if err := selForm.Run(); err != nil || target == "" {
				break
			}
		}
		ui.PrintInfo("Enabling TCP/IP mode on " + target + "...")
		wirelessSerial, err := adb.ConnectWireless(target)
		if err != nil {
			ui.PrintError(err.Error())
			ui.PrintWarning("Make sure the phone is on the same Wi-Fi network and USB debugging is enabled.")
			break
		}
		ui.PrintSuccess("Connected! Wireless serial: " + wirelessSerial)
		ui.PrintInfo("You can now unplug the USB cable.")
		ui.PrintInfo("Tip: register '" + wirelessSerial + "' via Signup to login over Wi-Fi without a cable.")

	case "pair":
		var ipPort, code string
		pForm := huh.NewForm(huh.NewGroup(
			huh.NewInput().Title("Pairing IP:Port (shown on phone under Wireless debugging)").Placeholder("192.168.1.10:37281").Value(&ipPort),
			huh.NewInput().Title("6-digit Pairing Code").Placeholder("123456").Value(&code),
		))
		if err := pForm.Run(); err != nil || ipPort == "" || code == "" {
			break
		}
		ui.PrintInfo("Pairing with " + ipPort + "...")
		if err := adb.PairWireless(ipPort, code); err != nil {
			ui.PrintError(err.Error())
		} else {
			ui.PrintSuccess("Paired! Now use 'Connect directly to IP:Port' with the main port shown on your phone.")
		}

	case "direct":
		fmt.Print("Enter IP:Port to connect (e.g. 192.168.1.50:5555): ")
		target := ui.ReadLine()
		if target != "" {
			ui.PrintInfo("Connecting to " + target + "...")
			if err := adb.ConnectDirect(target); err != nil {
				ui.PrintError(err.Error())
			} else {
				ui.PrintSuccess("Connected to " + target + "! You can now select it in adbgo.")
			}
		}

	case "scan":
		ui.PrintInfo("Scanning mDNS for wireless devices...")
		targets, err := adb.DiscoverMDNS()
		if err != nil {
			ui.PrintError(err.Error())
		} else if len(targets) == 0 {
			ui.PrintWarning("No mDNS wireless devices found. Check if Wireless Debugging is enabled on phone.")
		} else {
			ui.PrintSuccess(fmt.Sprintf("Found %d wireless target(s):", len(targets)))
			for _, t := range targets {
				fmt.Println("  • " + t)
			}
		}

	case "share_lan":
		share.Run("lan")

	case "share_wan":
		share.Run("wan")

	case "disconnect":
		devices, err := adb.ListDevices()
		if err != nil {
			ui.PrintError(err.Error())
			break
		}
		var wireless []adb.DeviceInfo
		for _, d := range devices {
			if strings.Contains(d.Serial, ":") {
				wireless = append(wireless, d)
			}
		}
		if len(wireless) == 0 {
			ui.PrintError("No wireless devices connected.")
			break
		}
		opts := make([]huh.Option[string], len(wireless))
		for i, d := range wireless {
			opts[i] = huh.NewOption(d.Serial, d.Serial)
		}
		var sel string
		selForm := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().Title("Disconnect which device?").Options(opts...).Value(&sel),
		))
		selForm.Run()
		if err := adb.DisconnectWireless(sel); err != nil {
			ui.PrintError(err.Error())
		} else {
			ui.PrintSuccess("Disconnected " + sel)
		}
	}
	if choice != "back" && choice != "" {
		ui.Pause()
	}
}

func showDeviceInfo(serial string) {
	out, err := adb.Run(serial, "shell", "getprop")
	if err != nil {
		ui.PrintError(err.Error())
		return
	}
	interesting := []string{
		"ro.product.model", "ro.build.version.release", "ro.serialno",
		"ro.product.manufacturer", "ro.build.version.sdk", "ro.product.cpu.abi",
	}
	for _, line := range strings.Split(out, "\n") {
		for _, key := range interesting {
			if strings.Contains(line, key) {
				fmt.Println(line)
			}
		}
	}
}

func showMultiDevice(session *auth.Session) {
	devices, err := adb.ListDevices()
	if err != nil {
		ui.PrintError(err.Error())
		ui.Pause()
		return
	}
	var online []adb.DeviceInfo
	for _, d := range devices {
		if d.Status == "device" {
			online = append(online, d)
		}
	}
	if len(online) == 0 {
		ui.PrintError("No online devices found.")
		ui.Pause()
		return
	}

	opts := make([]huh.Option[string], len(online))
	for i, d := range online {
		label := d.Serial
		if d.Model != "" {
			label = fmt.Sprintf("%s (%s)", d.Serial, d.Model)
		}
		opts[i] = huh.NewOption(label, d.Serial)
	}

	var selected []string
	devForm := huh.NewForm(
		huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Select devices (space to toggle, enter to confirm)").
				Options(opts...).
				Value(&selected),
		),
	)
	if err := devForm.Run(); err != nil || len(selected) == 0 {
		return
	}

	var action string
	actForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(fmt.Sprintf("Run against %d device(s)", len(selected))).
				Options(
					huh.NewOption("Wake screens", "wake"),
					huh.NewOption("Lock screens", "lock"),
					huh.NewOption("Reboot all", "reboot"),
					huh.NewOption("Install APK on all devices", "install_all"),
					huh.NewOption("Screenshots from all", "screenshot"),
					huh.NewOption("List user packages", "apps"),
					huh.NewOption("Cancel", "cancel"),
				).
				Value(&action),
		),
	)
	if err := actForm.Run(); err != nil || action == "cancel" {
		return
	}

	var sharedAPK string
	if action == "install_all" {
		fmt.Print("Path to APK to install on all devices: ")
		sharedAPK = ui.ReadLine()
		if sharedAPK == "" {
			return
		}
	}

	okCount := 0
	for _, serial := range selected {
		var err error
		switch action {
		case "wake":
			err = adb.WakeScreen(serial)
		case "lock":
			err = adb.LockScreen(serial)
		case "reboot":
			err = adb.Reboot(serial, "")
		case "screenshot":
			var file string
			file, err = adb.Screenshot(serial)
			if err == nil {
				ui.PrintSuccess(serial + " → " + file)
			}
		case "install_all":
			ui.PrintInfo("Installing to " + serial + "...")
			err = adb.InstallPackage(serial, sharedAPK, false)
		case "apps":
			var pkgs []string
			pkgs, err = adb.UserPackages(serial)
			if err == nil {
				ui.PrintInfo(serial + ": " + fmt.Sprint(len(pkgs)) + " user packages")
			}
		}
		if err != nil {
			ui.PrintError(serial + " failed: " + err.Error())
		} else {
			if action != "screenshot" && action != "apps" {
				ui.PrintSuccess(serial + " → done")
			}
			okCount++
		}
	}
	ui.PrintInfo(fmt.Sprintf("%d/%d devices succeeded.", okCount, len(selected)))
	ui.Pause()
}

func showDeviceManagement(cfg *config.Config) {
	ui.ClearScreen()
	fmt.Println(ui.TitleStyle.Render("adbgo — Device Management"))
	fmt.Println()

	if len(cfg.Devices) == 0 {
		ui.PrintError("No devices registered yet.")
		ui.Pause()
		return
	}

	for {
		opts := make([]huh.Option[string], 0, len(cfg.Devices)+1)
		for _, d := range cfg.Devices {
			label := fmt.Sprintf("%s (%s) — registered %s", d.Name, d.Serial, d.Created.Format("2006-01-02"))
			opts = append(opts, huh.NewOption(label, d.Serial))
		}
		opts = append(opts, huh.NewOption("Back", "back"))

		var sel string
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().Title("Registered devices").Options(opts...).Value(&sel),
		))
		if err := form.Run(); err != nil || sel == "back" {
			return
		}

		dev := cfg.FindBySerial(sel)
		if dev == nil {
			return
		}

		var action string
		actForm := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title(fmt.Sprintf("Manage '%s' (%s)", dev.Name, dev.Serial)).
				Options(
					huh.NewOption("Rename", "rename"),
					huh.NewOption("Remove registration", "remove"),
					huh.NewOption("Back", "back"),
				).
				Value(&action),
		))
		actForm.Run()

		switch action {
		case "rename":
			var newName string
			nameForm := huh.NewForm(huh.NewGroup(
				huh.NewInput().Title("New friendly name").Value(&newName),
			))
			if err := nameForm.Run(); err != nil || strings.TrimSpace(newName) == "" {
				break
			}
			cfg.RenameDevice(sel, strings.TrimSpace(newName))
			if err := cfg.Save(); err != nil {
				ui.PrintError(err.Error())
			} else {
				ui.PrintSuccess("Renamed to " + newName)
			}
		case "remove":
			if danger.ConfirmYES("Remove registration for " + sel + " (" + dev.Name + ")") {
				cfg.RemoveDevice(sel)
				if err := cfg.Save(); err != nil {
					ui.PrintError(err.Error())
				} else {
					ui.PrintSuccess("Removed " + sel)
					if len(cfg.Devices) == 0 {
						return
					}
				}
			} else {
				ui.PrintError("Cancelled")
			}
		}
	}
}

func doExport(cfg *config.Config) {
	path, err := config.ConfigPath()
	if err != nil {
		ui.PrintError(err.Error())
		ui.Pause()
		return
	}
	if err := copyFile(path, "adbgo-devices-export.json"); err != nil {
		ui.PrintError("Export failed: " + err.Error())
	} else {
		ui.PrintSuccess("Exported to adbgo-devices-export.json")
		ui.PrintWarning("This file contains password hashes — store it safely.")
	}
	ui.Pause()
}

func doImport(cfg *config.Config) {
	fmt.Print("Path to exported devices file: ")
	path := ui.ReadLine()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		ui.PrintError("Import failed: " + err.Error())
		ui.Pause()
		return
	}
	var imported config.Config
	if err := json.Unmarshal(data, &imported); err != nil {
		ui.PrintError("Invalid file: " + err.Error())
		ui.Pause()
		return
	}
	added := 0
	for _, d := range imported.Devices {
		if cfg.FindBySerial(d.Serial) == nil {
			cfg.Devices = append(cfg.Devices, d)
			added++
		}
	}
	if err := cfg.Save(); err != nil {
		ui.PrintError(err.Error())
	} else {
		ui.PrintSuccess(fmt.Sprintf("Imported %d device(s).", added))
	}
	ui.Pause()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Sync()
}

func showDangerZone(session *auth.Session) {
	var choice string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(ui.WarningStyle.Render("DANGER ZONE — NUCLEAR OPTIONS")).
				Options(
					huh.NewOption("Interactive Root Shell (su with PTY)", "root"),
					huh.NewOption("Factory Reset / Wipe Data", "factory"),
					huh.NewOption("Wipe Cache", "cache"),
					huh.NewOption("Unlock Bootloader (fastboot)", "unlock"),
					huh.NewOption("Lock Bootloader (fastboot)", "lock"),
					huh.NewOption("Remove Screen Lock", "lockscreen"),
					huh.NewOption("🔓 Lockscreen Credential Tester (PIN / password / pattern)", "bruteforce"),
					huh.NewOption("Attempt Disable FRP", "frp"),
					huh.NewOption("Format /data", "format_data"),
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
	case "root":
		err = danger.FullRootShell(serial)
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
	case "bruteforce":
		bruteforceFlow(serial)
	case "frp":
		err = danger.DisableFRP(serial)
	case "format_data":
		err = danger.FormatData(serial)
	case "raw_adb":
		err = danger.RawADB(serial)
	case "raw_fastboot":
		err = danger.RawFastboot(serial)
	}

	if err != nil {
		ui.PrintError(err.Error())
	}
	if choice != "back" && choice != "" {
		ui.Pause()
	}
}

// launchAppFlow asks for a keyword (e.g. "spotify"), matches it against
// installed packages and launches the chosen app.
func launchAppFlow(serial string) {
	fmt.Print("Enter app name or keyword (e.g. spotify, whatsapp, chrome): ")
	keyword := ui.ReadLine()
	if keyword == "" {
		return
	}

	matches, err := adb.FindPackagesByKeyword(serial, keyword)
	if err != nil {
		ui.PrintError(err.Error())
		return
	}

	switch len(matches) {
	case 0:
		ui.PrintError("No installed app matches '" + keyword + "'")
		ui.PrintInfo("Tip: try a shorter keyword, or list packages first.")
		return
	case 1:
		if err := adb.LaunchApp(serial, matches[0]); err != nil {
			ui.PrintError("Launch failed: " + err.Error())
		} else {
			ui.PrintSuccess("Launched " + matches[0])
		}
	default:
		opts := make([]huh.Option[string], len(matches))
		for i, m := range matches {
			opts[i] = huh.NewOption(m, m)
		}
		var sel string
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title(fmt.Sprintf("%d apps match '%s' — pick one", len(matches), keyword)).
				Options(opts...).
				Value(&sel),
		))
		if err := form.Run(); err != nil {
			return
		}
		if err := adb.LaunchApp(serial, sel); err != nil {
			ui.PrintError("Launch failed: " + err.Error())
		} else {
			ui.PrintSuccess("Launched " + sel)
		}
	}
}

// bruteforceFlow guides an authorized lockscreen credential test:
// PIN, password or pattern candidates are verified against the device via
// the framework oracle, with throttle-aware backoff and resumable sessions.
func bruteforceFlow(serial string) {
	ui.ClearScreen()
	fmt.Println(ui.TitleStyle.Render("🔓 Lockscreen Credential Tester"))
	fmt.Println()
	ui.PrintWarning("Authorized use only: run this on your own device, or one you have written permission to test.")
	ui.PrintWarning("If 'Erase all data after 10 wrong attempts' is enabled, a failed run can FACTORY RESET the device.")
	fmt.Println()
	if !danger.ConfirmYES("run lockscreen credential testing on " + serial) {
		ui.PrintError("Cancelled")
		return
	}

	var typ string
	tForm := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Credential type").
			Options(
				huh.NewOption("PIN (numeric)", "pin"),
				huh.NewOption("Password (wordlist / charset)", "password"),
				huh.NewOption("Pattern (3x3 grid)", "pattern"),
			).
			Value(&typ),
	))
	if err := tForm.Run(); err != nil || typ == "" {
		return
	}

	ui.PrintInfo("Checking device support (locksettings verify)...")
	if !adb.CheckLockVerifySupport(serial) {
		ui.PrintError("This device does not expose locksettings verify (Android 10+ required).")
		ui.PrintInfo("The credential oracle is unavailable, so testing cannot proceed safely.")
		ui.Pause()
		return
	}
	ui.PrintSuccess("Credential oracle available: correct / incorrect / throttled are reported by the framework.")
	fmt.Println()

	cfg := adb.LockBruteConfig{
		Type:        typ,
		CommonFirst: true,
		Delay:       500 * time.Millisecond,
		Jitter:      150 * time.Millisecond,
		MaxAttempts: 0,
	}

	minS, maxS, maskS, wordS, delayS, attS := "4", "4", "", "", "500", "0"
	var resume bool

	var fields []huh.Field
	switch typ {
	case "pin":
		fields = append(fields,
			huh.NewInput().Title("Min PIN length").Value(&minS),
			huh.NewInput().Title("Max PIN length").Value(&maxS),
			huh.NewInput().Title("Mask (optional: ? = unknown digit, e.g. 08??)").Value(&maskS),
		)
	case "password":
		minS, maxS = "1", "6"
		fields = append(fields,
			huh.NewInput().Title("Wordlist file (blank = charset brute-force)").Value(&wordS),
			huh.NewInput().Title("Min length").Value(&minS),
			huh.NewInput().Title("Max length").Value(&maxS),
			huh.NewInput().Title("Mask (optional: ? = any character)").Value(&maskS),
		)
	case "pattern":
		minS, maxS = "3", "9"
		fields = append(fields,
			huh.NewInput().Title("Min pattern length (3-9)").Value(&minS),
			huh.NewInput().Title("Max pattern length (3-9)").Value(&maxS),
		)
	}
	fields = append(fields,
		huh.NewInput().Title("Delay between attempts (ms)").Value(&delayS),
		huh.NewInput().Title("Max attempts (0 = unlimited)").Value(&attS),
		huh.NewConfirm().Title("Resume previous session (skip already-tried candidates)?").Value(&resume),
	)
	if err := huh.NewForm(huh.NewGroup(fields...)).Run(); err != nil {
		return
	}

	cfg.MinLen, _ = strconv.Atoi(strings.TrimSpace(minS))
	cfg.MaxLen, _ = strconv.Atoi(strings.TrimSpace(maxS))
	cfg.Mask = strings.TrimSpace(maskS)
	cfg.Wordlist = strings.TrimSpace(wordS)
	if d, err := strconv.Atoi(strings.TrimSpace(delayS)); err == nil && d >= 0 {
		cfg.Delay = time.Duration(d) * time.Millisecond
	}
	if a, err := strconv.Atoi(strings.TrimSpace(attS)); err == nil && a > 0 {
		cfg.MaxAttempts = a
	}
	cfg.Resume = resume

	ui.ClearScreen()
	fmt.Println(ui.TitleStyle.Render("🔓 Brute-force running — Ctrl+C saves the session and exits"))
	fmt.Println()

	found, err := adb.RunLockBruteforce(serial, cfg, func(st adb.LockBruteStats) bool {
		state := "verifying"
		if st.Throttling {
			state = "throttled — waiting " + st.LastWait.Round(time.Second).String()
		}
		fmt.Printf("\r  [%s] tried %d | skipped %d | throttled %dx | rate %.1f/s | %s   ",
			state, st.Attempts, st.Skipped, st.Throttles, st.Rate(), st.Current)
		return true
	})
	fmt.Println()
	if err != nil {
		ui.PrintError(err.Error())
		if strings.Contains(err.Error(), "session saved") {
			ui.PrintInfo("Run this option again and choose Resume to continue where you left off.")
		}
	} else {
		ui.PrintSuccess("CREDENTIAL FOUND: " + found)
	}
	ui.Pause()
}

// showPentestLab is the recon entry point of the Pentest Lab: device
// fingerprinting and app attack-surface analysis, for authorized testing.
func showPentestLab(session *auth.Session) {
	serial := session.Device.Serial
	for {
		var choice string
		form := huh.NewForm(huh.NewGroup(
			huh.NewSelect[string]().
				Title("🛡️ Pentest Lab — " + session.Device.Name).
				Options(
					huh.NewOption("🎯 Device Fingerprint (security posture & verdict)", "fingerprint"),
					huh.NewOption("💥 Attack-Surface Dump (single app)", "surface"),
					huh.NewOption("💥 Attack-Surface Scan (all user apps, ranked)", "scan_all"),
					huh.NewOption("Back", "back"),
				).
				Value(&choice),
		))
		if err := form.Run(); err != nil || choice == "back" || choice == "" {
			return
		}

		switch choice {
		case "fingerprint":
			pentestFingerprintFlow(serial)
		case "surface":
			pentestSurfaceFlow(serial)
		case "scan_all":
			pentestScanAllFlow(serial)
		}
	}
}

// pentestFingerprintFlow profiles the device and shows a posture verdict.
func pentestFingerprintFlow(serial string) {
	ui.ClearScreen()
	fmt.Println(ui.TitleStyle.Render("🎯 Device Fingerprint"))
	fmt.Println()
	ui.PrintInfo("Profiling device (read-only queries)...")
	fp, err := adb.CollectFingerprint(serial)
	if err != nil {
		ui.PrintError(err.Error())
		ui.Pause()
		return
	}
	fmt.Println(fp.Report())
	fmt.Println(ui.Highlight.Render("Verdict: ") + fp.RiskVerdict())
	fmt.Println()

	var save bool
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("Save fingerprint as JSON report?").Value(&save),
	)).Run(); err == nil && save {
		saveJSONReport("adbgo-fingerprint", fp)
	}
	ui.Pause()
}

// pentestSurfaceFlow dumps the attack surface of one app.
func pentestSurfaceFlow(serial string) {
	fmt.Print("Enter package name (or keyword, e.g. whatsapp): ")
	q := ui.ReadLine()
	if q == "" {
		return
	}
	pkg := q
	if matches, err := adb.FindPackagesByKeyword(serial, q); err == nil {
		if len(matches) == 1 {
			pkg = matches[0]
		} else if len(matches) > 1 {
			opts := make([]huh.Option[string], len(matches))
			for i, m := range matches {
				opts[i] = huh.NewOption(m, m)
			}
			var sel string
			selForm := huh.NewForm(huh.NewGroup(
				huh.NewSelect[string]().
					Title(fmt.Sprintf("%d apps match '%s' — pick one", len(matches), q)).
					Options(opts...).
					Value(&sel),
			))
			if err := selForm.Run(); err != nil || sel == "" {
				return
			}
			pkg = sel
		}
	}

	ui.PrintInfo("Dumping package attack surface for " + pkg + "...")
	s, err := adb.ScanAppSurface(serial, pkg)
	if err != nil {
		ui.PrintError(err.Error())
		ui.Pause()
		return
	}
	fmt.Println()
	fmt.Println(s.Report())
	fmt.Println(ui.DimStyle.Render("Dangerous permissions: " + strings.Join(s.DangerousPermissions, ", ")))
	fmt.Println()

	var save bool
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("Save as JSON report?").Value(&save),
	)).Run(); err == nil && save {
		saveJSONReport("adbgo-surface", s)
	}
	ui.Pause()
}

// pentestScanAllFlow scans every user app and ranks them by findings.
func pentestScanAllFlow(serial string) {
	ui.ClearScreen()
	ui.PrintInfo("Scanning all user apps (this may take a while)...")
	surfaces, err := adb.ScanAllSurfaces(serial, func(done, total int, pkg string) {
		fmt.Printf("\r  scanning %d/%d: %-45s", done+1, total, pkg)
	})
	fmt.Println("\n")
	if err != nil {
		ui.PrintError(err.Error())
		ui.Pause()
		return
	}
	if len(surfaces) == 0 {
		ui.PrintWarning("No user apps found.")
		ui.Pause()
		return
	}

	withIssues := 0
	for _, s := range surfaces {
		if len(s.Issues()) > 0 {
			withIssues++
		}
	}
	fmt.Println(ui.TitleStyle.Render(fmt.Sprintf("💥 Attack-Surface Scan — %d apps scanned, %d with findings", len(surfaces), withIssues)))
	fmt.Println()
	shown := 0
	for _, s := range surfaces {
		if len(s.Issues()) == 0 {
			continue
		}
		if shown >= 15 {
			fmt.Printf("  ... and %d more apps with findings — see the JSON report\n", withIssues-shown)
			break
		}
		fmt.Println(s.Report())
		shown++
	}

	var save bool
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("Save full results as JSON report?").Value(&save),
	)).Run(); err == nil && save {
		saveJSONReport("adbgo-surface-scan", surfaces)
	}
	ui.Pause()
}

// saveJSONReport marshals v to a timestamped JSON file in the CWD.
func saveJSONReport(prefix string, v any) {
	path := prefix + "-" + time.Now().Format("20060102-150405") + ".json"
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		ui.PrintError("Could not marshal report: " + err.Error())
		return
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		ui.PrintError("Could not save report: " + err.Error())
		return
	}
	ui.PrintSuccess("Report saved: " + path)
}

// extractPhotosFlow pulls camera photos, screenshots, screen recordings,
// recent photos or all media from the phone into a local folder — either
// everything at once or a hand-picked selection, optionally cutting
// (deleting) them from the device after a successful transfer.
func extractPhotosFlow(serial string) {
	var cat string
	cForm := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Extract which photos?").
			Options(
				huh.NewOption("🕒 Recent photos (latest 30, newest first)", "recent"),
				huh.NewOption("📷 Camera photos (/sdcard/DCIM/Camera)", "camera"),
				huh.NewOption("🖼️ Screenshots (/sdcard/Pictures/Screenshots)", "screenshots"),
				huh.NewOption("🎬 Screen recordings (/sdcard/Movies/ScreenRecords)", "screenrec"),
				huh.NewOption("📚 All current photos (/sdcard/DCIM, recursive)", "all"),
			).
			Value(&cat),
	))
	if err := cForm.Run(); err != nil || cat == "" {
		return
	}

	remoteRoot := "/sdcard"
	var dirs []string
	switch cat {
	case "camera":
		remoteRoot = "/sdcard/DCIM/Camera"
		dirs = []string{remoteRoot}
	case "screenshots":
		remoteRoot = "/sdcard/Pictures/Screenshots"
		dirs = []string{remoteRoot}
	case "screenrec":
		remoteRoot = "/sdcard/Movies/ScreenRecords"
		dirs = []string{remoteRoot}
	case "all":
		remoteRoot = "/sdcard/DCIM"
		dirs = []string{remoteRoot}
	default: // recent: newest across the usual media locations
		dirs = []string{"/sdcard/DCIM/Camera", "/sdcard/Pictures/Screenshots", "/sdcard/Movies/ScreenRecords", "/sdcard/Pictures"}
	}

	ui.PrintInfo("Finding media on the phone...")
	var infos []adb.MediaInfo
	if cat == "recent" {
		infos = adb.RecentMedia(serial, dirs, 30)
	} else {
		var err error
		infos, err = adb.ListRemoteMediaInfo(serial, dirs[0])
		if err != nil {
			ui.PrintError(err.Error())
			ui.Pause()
			return
		}
	}
	if len(infos) == 0 {
		ui.PrintWarning("No media files found on the phone.")
		ui.Pause()
		return
	}
	ui.PrintSuccess(fmt.Sprintf("Found %d media file(s).", len(infos)))

	// All files or pick interactively?
	var pick string
	pForm := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().
			Title("Which files do you want?").
			Options(
				huh.NewOption(fmt.Sprintf("All %d file(s)", len(infos)), "all"),
				huh.NewOption("👀 Pick files (scrollable list, select the ones you want)", "pick"),
				huh.NewOption("🖼 Gallery (browse thumbnails, select visually)", "gallery"),
			).
			Value(&pick),
	))
	if err := pForm.Run(); err != nil {
		return
	}

	transfer := infos
	if pick == "pick" {
		opts := make([]huh.Option[string], len(infos))
		for i, f := range infos {
			label := fmt.Sprintf("%s — %s — %s", f.Name, adb.HumanSize(f.Size), time.Unix(f.Mtime, 0).Format("2006-01-02 15:04"))
			opts[i] = huh.NewOption(label, f.Path)
		}
		var chosen []string
		pickForm := huh.NewForm(huh.NewGroup(
			huh.NewMultiSelect[string]().
				Title("Scroll with ↑/↓, space to select, / to filter, enter to confirm").
				Filterable(true).
				Options(opts...).
				Value(&chosen),
		))
		if err := pickForm.Run(); err != nil {
			return
		}
		chosenSet := make(map[string]bool, len(chosen))
		for _, c := range chosen {
			chosenSet[c] = true
		}
		transfer = transfer[:0]
		for _, f := range infos {
			if chosenSet[f.Path] {
				transfer = append(transfer, f)
			}
		}
		if len(transfer) == 0 {
			ui.PrintWarning("Nothing selected.")
			return
		}
	}
	if pick == "gallery" {
		galleryFiles := infos
		if len(galleryFiles) > 60 {
			ui.PrintWarning("Gallery shows the newest 60 files — use 'Pick files' for the full list.")
		}
		if len(galleryFiles) > 60 {
			galleryFiles = galleryFiles[:60]
		}
		items := prepareGalleryItems(serial, galleryFiles)
		m := galleryModel{items: items, sel: map[int]bool{}}
		outModel, err := tea.NewProgram(m).Run()
		if err != nil {
			ui.PrintError(err.Error())
			return
		}
		gm, ok := outModel.(galleryModel)
		if !ok || !gm.done {
			ui.PrintWarning("Cancelled")
			return
		}
		transfer = transfer[:0]
		for i := range galleryFiles {
			if gm.sel[i] {
				transfer = append(transfer, galleryFiles[i])
			}
		}
		if len(transfer) == 0 {
			ui.PrintWarning("Nothing selected.")
			return
		}
	}
	ui.PrintInfo(fmt.Sprintf("%d file(s) selected.", len(transfer)))

	// Optional preview in the system image viewer before transferring.
	var preview bool
	pvForm := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().
			Title("Preview selected file(s) in your image viewer first?").
			Value(&preview),
	))
	if err := pvForm.Run(); err != nil {
		return
	}
	if preview {
		openMediaPreview(serial, transfer)
		var proceed bool
		if err := huh.NewForm(huh.NewGroup(
			huh.NewConfirm().Title(fmt.Sprintf("Continue transferring %d file(s)?", len(transfer))).Value(&proceed),
		)).Run(); err != nil || !proceed {
			ui.PrintError("Cancelled")
			return
		}
	}

	var dest string
	var mode string
	dForm := huh.NewForm(huh.NewGroup(
		huh.NewInput().
			Title("Destination folder on PC").
			Placeholder("adbgo-photos").
			Value(&dest),
		huh.NewSelect[string]().
			Title("Copy or cut?").
			Options(
				huh.NewOption("Copy (keep photos on phone)", "copy"),
				huh.NewOption("Cut (delete from phone after transfer)", "cut"),
			).
			Value(&mode),
	))
	if err := dForm.Run(); err != nil {
		return
	}
	dest = strings.TrimSpace(dest)
	if dest == "" {
		dest = "adbgo-photos"
	}
	move := mode == "cut"

	if move && !danger.ConfirmYES(fmt.Sprintf("CUT %d file(s) — they will be DELETED from the phone after transfer", len(transfer))) {
		ui.PrintError("Cancelled")
		return
	}

	pulled, moved, failed, err := adb.PullFiles(serial, transfer, remoteRoot, dest, move, func(done, total int, name string) {
		fmt.Printf("\r  transferring %d/%d: %-45s", done, total, name)
	})
	fmt.Println()
	if err != nil {
		ui.PrintError(err.Error())
		ui.Pause()
		return
	}
	if pulled == 0 {
		ui.PrintWarning("Transfer failed — nothing was copied, and nothing was deleted from the phone.")
		ui.Pause()
		return
	}
	ui.PrintSuccess(fmt.Sprintf("Copied %d file(s) to %s", pulled, dest))
	if move {
		ui.PrintSuccess(fmt.Sprintf("Deleted %d file(s) from the phone (cut)", moved))
	}
	if failed > 0 {
		ui.PrintWarning(fmt.Sprintf("%d file(s) had problems — anything not fully transferred was NOT deleted from the phone.", failed))
	}

	var openFolder bool
	if err := huh.NewForm(huh.NewGroup(
		huh.NewConfirm().Title("Open destination folder?").Value(&openFolder),
	)).Run(); err == nil && openFolder {
		_ = exec.Command("xdg-open", dest).Start()
	}
	ui.Pause()
}

// openMediaPreview pulls up to 12 selected media files into a temp
// folder and opens them with the system image viewer (xdg-open), so
// they can be inspected before transferring.
func openMediaPreview(serial string, files []adb.MediaInfo) {
	if len(files) > 12 {
		ui.PrintWarning(fmt.Sprintf("Opening only the first 12 of %d selected file(s).", len(files)))
		files = files[:12]
	}
	dir := filepath.Join(os.TempDir(), "adbgo-preview")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		ui.PrintError("Could not create preview folder: " + err.Error())
		return
	}
	opened := 0
	for _, f := range files {
		local := filepath.Join(dir, f.Name)
		if _, err := adb.Run(serial, "pull", f.Path, local); err != nil {
			ui.PrintWarning("Could not pull " + f.Name + ": " + err.Error())
			continue
		}
		if err := exec.Command("xdg-open", local).Start(); err != nil {
			ui.PrintWarning("Could not open " + f.Name + ": " + err.Error())
			continue
		}
		opened++
	}
	if opened > 0 {
		ui.PrintInfo(fmt.Sprintf("Opened %d preview(s) in your image viewer — close them when done.", opened))
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Thumbnail gallery (bubbletea): browse media as truecolor half-block
// thumbnails, toggle selection visually, then confirm.
// ─────────────────────────────────────────────────────────────────────────────

const (
	galCols  = 4 // thumbnails per row
	galRows  = 3 // rows per page (12 per page)
	thumbW   = 24 // cells wide
	thumbH   = 10 // cells tall (20 pixel rows)
)

type galleryItem struct {
	info  adb.MediaInfo
	local string // temp copy on PC used for the thumbnail / full preview
	thumb string // pre-rendered ANSI thumbnail
}

type galleryModel struct {
	items  []galleryItem
	sel    map[int]bool
	cursor int
	done   bool
}

func (m galleryModel) Init() tea.Cmd { return nil }

func (m galleryModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "enter":
			m.done = true
			return m, tea.Quit
		case "right", "l":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "left", "h":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor+galCols < len(m.items) {
				m.cursor += galCols
			}
		case "up", "k":
			if m.cursor-galCols >= 0 {
				m.cursor -= galCols
			}
		case "pgdown":
			if m.cursor+galCols*galRows < len(m.items) {
				m.cursor += galCols * galRows
			}
		case "pgup":
			if m.cursor-galCols*galRows >= 0 {
				m.cursor -= galCols * galRows
			}
		case " ":
			m.sel[m.cursor] = !m.sel[m.cursor]
		case "a":
			for i := range m.items {
				m.sel[i] = true
			}
		case "n":
			m.sel = map[int]bool{}
		case "o":
			if m.cursor < len(m.items) {
				_ = exec.Command("xdg-open", m.items[m.cursor].local).Start()
			}
		}
	}
	return m, nil
}

func (m galleryModel) View() string {
	if len(m.items) == 0 {
		return "No media to display."
	}
	perPage := galCols * galRows
	pageStart := (m.cursor / perPage) * perPage

	selected := 0
	for _, v := range m.sel {
		if v {
			selected++
		}
	}

	var b strings.Builder
	b.WriteString(ui.TitleStyle.Render(fmt.Sprintf("🖼 adbgo gallery — %d/%d  selected: %d", m.cursor+1, len(m.items), selected)))
	b.WriteString("\n")

	for row := 0; row < galRows; row++ {
		for line := 0; line < thumbH; line++ {
			for col := 0; col < galCols; col++ {
				idx := pageStart + row*galCols + col
				if idx >= len(m.items) {
					break
				}
				thumb := m.items[idx].thumb
				lines := strings.Split(thumb, "\n")
				if line < len(lines) {
					b.WriteString(lines[line])
				}
				b.WriteString("  ")
			}
			b.WriteString("\n")
		}
		// captions under each thumbnail of this row
		for col := 0; col < galCols; col++ {
			idx := pageStart + row*galCols + col
			if idx >= len(m.items) {
				break
			}
			mark := " "
			if m.sel[idx] {
				mark = "✓"
			}
			cap := fmt.Sprintf("%s %2d %s", mark, idx+1, m.items[idx].info.Name)
			if len(cap) > thumbW {
				cap = cap[:thumbW-1] + "…"
			}
			if idx == m.cursor {
				cap = "\x1b[7m" + cap + "\x1b[0m"
			}
			b.WriteString(cap)
			b.WriteString("  ")
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(ui.DimStyle.Render("↑↓←→/hjkl move · space select · a all · n none · o open full size · pgup/pgdn page · enter confirm · q cancel"))
	return b.String()
}

// prepareGalleryItems pulls each media file to a temp folder and renders
// a small truecolor half-block thumbnail for it (videos and undecodable
// formats get a placeholder).
func prepareGalleryItems(serial string, files []adb.MediaInfo) []galleryItem {
	dir := filepath.Join(os.TempDir(), "adbgo-gallery")
	os.MkdirAll(dir, 0o755)
	items := make([]galleryItem, 0, len(files))
	for i, f := range files {
		fmt.Printf("\r  thumbnail %d/%d: %-45s", i+1, len(files), f.Name)
		local := filepath.Join(dir, fmt.Sprintf("%03d_%s", i, f.Name))
		item := galleryItem{info: f, local: local}
		if _, err := adb.Run(serial, "pull", f.Path, local); err == nil {
			if img, err := loadImage(local); err == nil {
				item.thumb = renderThumb(img, thumbW, thumbH)
			}
		}
		if item.thumb == "" {
			item.thumb = placeholderThumb(f)
		}
		items = append(items, item)
	}
	fmt.Println()
	return items
}

// loadImage decodes a jpeg/png/gif file into an image.Image.
func loadImage(path string) (image.Image, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	img, _, derr := image.Decode(fh)
	if derr != nil {
		return nil, derr
	}
	return img, nil
}

// sampleColor nearest-neighbor samples the source image at (x,y) in a
// pixel space of pw x ph.
func sampleColor(img image.Image, x, y, pw, ph int) (int, int, int) {
	b := img.Bounds()
	sx := b.Min.X + x*b.Dx()/pw
	sy := b.Min.Y + y*b.Dy()/ph
	r, g, bl, _ := img.At(sx, sy).RGBA()
	return int(r >> 8), int(g >> 8), int(bl >> 8)
}

// renderThumb draws an image as truecolor half-blocks: each terminal cell
// holds two stacked pixels (▀ with fg = top pixel, bg = bottom pixel).
func renderThumb(img image.Image, w, h int) string {
	pw, ph := w*2, h*2
	var b strings.Builder
	for y := 0; y < ph; y += 2 {
		for x := 0; x < pw; x++ {
			tr, tg, tb := sampleColor(img, x, y, pw, ph)
			br, bg, bb := sampleColor(img, x, y+1, pw, ph)
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", tr, tg, tb, br, bg, bb)
		}
		b.WriteString("\x1b[0m\n")
	}
	return b.String()
}

// placeholderThumb is shown for videos and undecodable files.
func placeholderThumb(f adb.MediaInfo) string {
	var b strings.Builder
	for y := 0; y < thumbH-1; y++ {
		b.WriteString("\x1b[90m" + strings.Repeat("░", thumbW*2-2) + "  \x1b[0m\n")
	}
	label := "no preview"
	pad := (thumbW*2 - 2 - len(label)) / 2
	if pad < 0 {
		pad = 0
	}
	b.WriteString("\x1b[90m" + strings.Repeat("░", pad) + " " + label + " " + strings.Repeat("░", pad) + "\x1b[0m\n")
	return b.String()
}
