package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"adbgo/internal/adb"
	"adbgo/internal/auth"
	"adbgo/internal/config"
	"adbgo/internal/danger"
	"adbgo/internal/ui"
	"adbgo/internal/update"

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
					huh.NewOption("Device Management (rename / remove)", "manage"),
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
	case "manage":
		showDeviceManagement(cfg)
		return showAuthMenu(cfg)
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
		// Small delay to slow down brute-force attempts
		time.Sleep(1 * time.Second)
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
			huh.NewOption("File & System (shell, logcat, files, wireless)", "files"),
			huh.NewOption("Multi-Device Mode", "multi"),
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
			huh.NewOption("Check for Updates", "update"),
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
				ui.PrintWarning("Is scrcpy installed? (pacman -S scrcpy / apt install scrcpy)")
			}
			ui.Pause()

		case "device":
			showDeviceControl(session)

		case "apps":
			showAppManagement(session)

		case "files":
			showFileSystem(session)

		case "multi":
			showMultiDevice(session)

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

// checkForUpdates compares the running version with the latest GitHub
// release and offers a one-shot self-update.
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

// enableGodMode requires the account password — "YES" alone is not enough
// to unlock nuclear options.
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
					huh.NewOption("Record Screen", "record"),
					huh.NewOption("Wake Screen", "wake"),
					huh.NewOption("Lock Screen", "lock"),
					huh.NewOption("Back", "back"),
				).
				Value(&choice),
		),
	)
	form.Run()

	serial := session.Device.Serial
	switch choice {
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
					huh.NewOption("List user packages", "list_user"),
					huh.NewOption("List all packages", "list_all"),
					huh.NewOption("Install APK (sideload)", "install"),
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
		out, err := adb.Run(serial, "shell", "pm", "list", "packages", "-3")
		if err != nil {
			ui.PrintError(err.Error())
		} else {
			fmt.Println(out)
		}
	case "list_all":
		out, err := adb.Run(serial, "shell", "pm", "list", "packages")
		if err != nil {
			ui.PrintError(err.Error())
		} else {
			fmt.Println(out)
		}
	case "install":
		installAPKFlow(serial)
	case "force_stop", "clear", "uninstall":
		fmt.Print("Enter package name: ")
		pkg := ui.ReadLine()
		if pkg == "" {
			return
		}
		var err error
		switch choice {
		case "force_stop":
			_, err = adb.Run(serial, "shell", "am", "force-stop", pkg)
			if err == nil {
				ui.PrintSuccess("Force-stopped " + pkg)
			}
		case "clear":
			_, err = adb.Run(serial, "shell", "pm", "clear", pkg)
			if err == nil {
				ui.PrintSuccess("Cleared data for " + pkg)
			}
		case "uninstall":
			_, err = adb.Run(serial, "uninstall", pkg)
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

// installAPKFlow sideloads an APK picked from the local filesystem with
// streamed install output.
func installAPKFlow(serial string) {
	var apkPath string
	var downgrade bool

	picker := huh.NewFilePicker().
		Title("Choose an APK to install").
		AllowedTypes([]string{".apk", ".apkm", ".apks"}).
		Value(&apkPath)

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

	if strings.TrimSpace(apkPath) == "" {
		// Picker unavailable/cancelled — fall back to typing the path
		fmt.Print("Enter path to APK file: ")
		apkPath = ui.ReadLine()
		if apkPath == "" {
			return
		}
	}

	ui.PrintInfo("Installing " + apkPath + " (streamed output):")
	if err := adb.InstallAPK(serial, apkPath, downgrade); err != nil {
		ui.PrintError("Install failed: " + err.Error())
	} else {
		ui.PrintSuccess("Installed " + apkPath)
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
					huh.NewOption("Live Logcat (filter by package / priority)", "logcat"),
					huh.NewOption("Browse device files (push / pull)", "browse"),
					huh.NewOption("Wireless ADB (connect over Wi-Fi)", "wireless"),
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
		if err := adb.RunStreaming(serial, "shell"); err != nil {
			ui.PrintError("Shell ended with error: " + err.Error())
		}
	case "logcat":
		logcatFlow(serial)
	case "browse":
		browseFlow(serial)
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
	var pkg string
	var priority string
	var clearFirst bool

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().
				Title("Filter by package (blank = all logs)").
				Placeholder("com.example.app").
				Value(&pkg),
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
		Priority:   priority,
		ClearFirst: clearFirst,
	}); err != nil {
		ui.PrintError("Logcat stopped with error: " + err.Error())
	}
	fmt.Println()
}

// browseFlow is an interactive remote file manager rooted at /sdcard.
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
			fmt.Print("Local path to push: ")
			local := ui.ReadLine()
			if local == "" {
				continue
			}
			ui.PrintInfo("Pushing " + local + " → " + path)
			if err := adb.Push(serial, local, path); err != nil {
				ui.PrintError(err.Error())
			} else {
				ui.PrintSuccess("Pushed " + local)
			}
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
		default:
			// An entry was selected
			entryPath := path + "/" + action
			entry := findEntry(entries, action)
			if entry == nil {
				continue
			}
			if entry.IsDir {
				path = entryPath
				continue
			}

			// File selected: what to do with it?
			var fileAction string
			fileForm := huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title(entryPath).
						Options(
							huh.NewOption("⬇ Pull (download)", "pull"),
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

// wirelessFlow connects the device over Wi-Fi (adb tcpip 5555 → adb connect).
func wirelessFlow(serial string) {
	var choice string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Wireless ADB — " + serial).
				Options(
					huh.NewOption("Connect this device over Wi-Fi", "connect"),
					huh.NewOption("Disconnect a wireless device", "disconnect"),
					huh.NewOption("Back", "back"),
				).
				Value(&choice),
		),
	)
	form.Run()

	switch choice {
	case "connect":
		ui.PrintInfo("Reading device Wi-Fi IP...")
		wirelessSerial, err := adb.ConnectWireless(serial)
		if err != nil {
			ui.PrintError(err.Error())
			ui.PrintWarning("Make sure the phone is on the same Wi-Fi network and USB debugging is on.")
			break
		}
		ui.PrintSuccess("Connected! Wireless serial: " + wirelessSerial)
		ui.PrintInfo("You can now unplug the USB cable.")
		ui.PrintInfo("Tip: register '" + wirelessSerial + "' via Signup to login over Wi-Fi next time.")
	case "disconnect":
		devices, err := adb.ListDevices()
		if err != nil {
			ui.PrintError(err.Error())
			break
		}
		var wireless []adb.DeviceInfo
		for _, d := range devices {
			if strings.Contains(d.Serial, ":5555") {
				wireless = append(wireless, d)
			}
		}
		if len(wireless) == 0 {
			ui.PrintError("No wireless (tcpip) devices connected.")
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

// showMultiDevice runs an action against several connected devices at once.
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

	// Device multi-select
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
					huh.NewOption("Reboot to Recovery", "recovery"),
					huh.NewOption("Reboot to Bootloader", "bootloader"),
					huh.NewOption("Screenshots", "screenshot"),
					huh.NewOption("List user packages", "apps"),
					huh.NewOption("Cancel", "cancel"),
				).
				Value(&action),
		),
	)
	if err := actForm.Run(); err != nil || action == "cancel" {
		return
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
		case "recovery":
			err = adb.Reboot(serial, "recovery")
		case "bootloader":
			err = adb.Reboot(serial, "bootloader")
		case "screenshot":
			var file string
			file, err = adb.Screenshot(serial)
			if err == nil {
				ui.PrintSuccess(serial + " → " + file)
			}
		case "apps":
			var pkgs []string
			pkgs, err = adb.UserPackages(serial)
			if err == nil {
				ui.PrintInfo(serial + ": " + fmt.Sprint(len(pkgs)) + " user packages")
				for i, p := range pkgs {
					if i >= 15 {
						ui.PrintInfo("... and " + fmt.Sprint(len(pkgs)-15) + " more")
						break
					}
					fmt.Println("  " + p)
				}
			}
		}
		if err != nil {
			ui.PrintError(serial + " failed: " + err.Error())
		} else {
			switch action {
			case "wake", "lock", "reboot", "recovery", "bootloader":
				ui.PrintSuccess(serial + " → done")
			}
			okCount++
		}
	}
	ui.PrintInfo(fmt.Sprintf("%d/%d devices succeeded.", okCount, len(selected)))
	ui.Pause()
}

// showDeviceManagement renames or removes registered devices.
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
		case "back":
			// loop back to device list
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
	if choice != "back" && choice != "" {
		ui.Pause()
	}
}
