package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"adbgo/internal/adb"
	"adbgo/internal/auth"
	"adbgo/internal/config"
	"adbgo/internal/danger"
	"adbgo/internal/ui"

	"github.com/charmbracelet/huh"
)

var version = "dev"

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "--version", "-v", "version":
			fmt.Printf("adbgo %s\n", version)
			return
		case "--help", "-h", "help":
			fmt.Println("adbgo — terminal Android device toolkit")
			fmt.Println("Usage: adbgo [--version | --help]")
			fmt.Println("Run without arguments to open the interactive terminal UI.")
			return
		default:
			fmt.Fprintf(os.Stderr, "unknown argument: %s\nUse --help for usage.\n", os.Args[1])
			os.Exit(2)
		}
	}
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
	if len(password) < 10 {
		ui.PrintError("Password too short (min 10 characters)")
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
			huh.NewOption("Wireless ADB", "wireless"),
			huh.NewOption("Live Logcat", "logcat"),
			huh.NewOption("Install APK", "install_apk"),
			huh.NewOption("Device File Manager", "file_manager"),
			huh.NewOption("Screen Recording", "record"),
			huh.NewOption("Multi-device Tools", "multi"),
			huh.NewOption("Manage Registered Devices", "manage_devices"),
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
		case "wireless":
			wirelessADB(session)
		case "logcat":
			showLogcat(session)
		case "install_apk":
			installAPK(session)
		case "file_manager":
			fileManager(session)
		case "record":
			recordScreen(session)
		case "multi":
			multiDeviceTools()
		case "manage_devices":
			manageRegisteredDevices(cfg, session)

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
	ui.PrintWarning("GOD MODE exposes destructive device operations.")
	var password string
	form := huh.NewForm(huh.NewGroup(huh.NewInput().Title("Re-enter your adbgo password").EchoMode(huh.EchoModePassword).Value(&password)))
	if err := form.Run(); err != nil { return }
	if !auth.CheckPassword(session.Device.Password, password) {
		ui.PrintError("Incorrect password; GOD MODE remains disabled")
		ui.Pause()
		return
	}
	ui.PrintWarning("Only use on a device you own or are authorized to administer.")
	fmt.Print("Type YES to enable GOD MODE: ")
	var input string
	if _, err := fmt.Scanln(&input); err == nil && strings.TrimSpace(input) == "YES" {
		session.GodMode = true
		ui.PrintSuccess("GOD MODE enabled for this session")
	} else { ui.PrintError("Cancelled") }
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
	if err := form.Run(); err != nil { return }

	serial := session.Device.Serial
	var err error
	switch choice {
	case "reboot":
		err = adb.Reboot(serial, "")
	case "recovery":
		err = adb.Reboot(serial, "recovery")
	case "bootloader":
		err = adb.Reboot(serial, "bootloader")
	case "poweroff":
		_, err = adb.Run(serial, "shell", "reboot", "-p")
	case "screenshot":
		filename := fmt.Sprintf("screenshot-%s.png", time.Now().Format("20060102-150405"))
		remote := "/sdcard/" + filename
		_, err = adb.Run(serial, "shell", "screencap", "-p", remote)
		if err == nil {
			_, err = adb.Pull(context.Background(), serial, remote, filename)
		}
		if err == nil { ui.PrintSuccess("Screenshot saved as " + filename) }
	}
	if err != nil { ui.PrintError("Device operation failed: " + err.Error()) } else if choice != "back" && choice != "" && choice != "screenshot" {
		ui.PrintSuccess("Command sent to " + serial)
	}
	if choice != "back" { ui.Pause() }
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
	if err := form.Run(); err != nil || choice == "back" { return }

	serial := session.Device.Serial
	switch choice {
	case "list_user", "list_all":
		args := []string{"shell", "pm", "list", "packages"}
		if choice == "list_user" { args = append(args, "-3") }
		out, err := adb.Run(serial, args...)
		if err != nil { ui.PrintError("Could not list packages: " + err.Error()) } else { fmt.Println(out) }
	case "force_stop", "clear", "uninstall":
		var pkg string
		form := huh.NewForm(huh.NewGroup(huh.NewInput().Title("Android package name").Placeholder("com.example.app").Value(&pkg)))
		if err := form.Run(); err != nil { return }
		pkg = strings.TrimSpace(pkg)
		if pkg == "" || strings.ContainsAny(pkg, " \t\r\n") || strings.HasPrefix(pkg, "-") {
			ui.PrintError("Enter a valid package name.")
			ui.Pause()
			return
		}
		if choice == "clear" || choice == "uninstall" {
			verb := "clear all app data for"
			if choice == "uninstall" { verb = "uninstall" }
			var confirmed bool
			form = huh.NewForm(huh.NewGroup(huh.NewConfirm().Title(fmt.Sprintf("Are you sure you want to %s %s on %s?", verb, pkg, serial)).Affirmative("Continue").Negative("Cancel").Value(&confirmed)))
			if err := form.Run(); err != nil || !confirmed { ui.PrintWarning("Cancelled."); return }
		}
		var args []string
		switch choice {
		case "force_stop": args = []string{"shell", "am", "force-stop", pkg}
		case "clear": args = []string{"shell", "pm", "clear", pkg}
		case "uninstall": args = []string{"uninstall", pkg}
		}
		out, err := adb.Run(serial, args...)
		if err != nil { ui.PrintError("App operation failed: " + err.Error()) } else { fmt.Println(out); ui.PrintSuccess("ADB completed the requested app operation.") }
	}
	ui.Pause()
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
	if err := form.Run(); err != nil { return }

	serial := session.Device.Serial
	switch choice {
	case "shell":
		ui.PrintWarning("Starting adb shell. Type 'exit' to return.")
		cmd := exec.Command("adb", "-s", serial, "shell")
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil { ui.PrintError("Shell exited: " + err.Error()) }
	case "info":
		out, err := adb.Run(serial, "shell", "getprop")
		if err != nil { ui.PrintError("Could not read device properties: " + err.Error()); ui.Pause(); return }
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
					huh.NewOption("Reboot to Recovery (manual recovery menu)", "recovery"),
					huh.NewOption("Unlock Bootloader", "unlock"),
					huh.NewOption("Lock Bootloader", "lock"),
					huh.NewOption("Interactive Root Shell", "root"),
					huh.NewOption("Raw adb command", "raw_adb"),
					huh.NewOption("Raw fastboot command", "raw_fastboot"),
					huh.NewOption("Back", "back"),
				).
				Value(&choice),
		),
	)
	if err := form.Run(); err != nil { return }

	serial := session.Device.Serial
	var err error

	switch choice {
	case "recovery":
		err = adb.Reboot(serial, "recovery")
	case "unlock":
		err = danger.UnlockBootloader(serial)
	case "lock":
		err = danger.LockBootloader(serial)
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


// pickLocalFile uses a native file chooser when available, with a terminal fallback.
func pickLocalFile(title string) string {
	for _, chooser := range []string{"zenity", "kdialog"} {
		if _, err := exec.LookPath(chooser); err != nil { continue }
		var cmd *exec.Cmd
		if chooser == "zenity" { cmd = exec.Command(chooser, "--file-selection", "--title="+title, "--file-filter=APK files | *.apk") } else { cmd = exec.Command(chooser, "--getopenfilename", ".", "*.apk") }
		out, err := cmd.Output()
		if err == nil { return strings.TrimSpace(string(out)) }
		return ""
	}
	var path string
	form := huh.NewForm(huh.NewGroup(huh.NewInput().Title(title+" (full path)").Placeholder("/home/user/app.apk").Value(&path)))
	if form.Run() != nil { return "" }
	return strings.TrimSpace(path)
}

func wirelessADB(session *auth.Session) {
	devices, err := adb.ListDevices()
	if err != nil { ui.PrintError(err.Error()); ui.Pause(); return }
	var options []huh.Option[string]
	for _, d := range devices { if d.Status == "device" && !strings.Contains(d.Serial, ":") { options = append(options, huh.NewOption(d.Serial+" ("+d.Model+")", d.Serial)) } }
	if len(options) == 0 { ui.PrintError("Connect and authorize the phone over USB first."); ui.Pause(); return }
	serial := session.Device.Serial
	if !adb.IsConnected(serial) || strings.Contains(serial, ":") {
		form := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("USB-connected device").Options(options...).Value(&serial)))
		if form.Run() != nil { return }
	}
	var hostPort string
	form := huh.NewForm(huh.NewGroup(huh.NewInput().Title("Phone Wi-Fi IP:port").Placeholder("192.168.1.20:5555").Value(&hostPort)))
	if form.Run() != nil { return }
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second); defer cancel()
	if err := adb.ConnectTCPIP(ctx, serial, hostPort); err != nil { ui.PrintError(err.Error()) } else { ui.PrintSuccess("Wireless ADB connected: "+hostPort); ui.PrintWarning("On modern Android, prefer Settings → Developer options → Wireless debugging → Pair device.") }
	ui.Pause()
}

func showLogcat(session *auth.Session) {
	var priority, pkg string
	form := huh.NewForm(huh.NewGroup(
		huh.NewSelect[string]().Title("Minimum log priority").Options(huh.NewOption("Verbose", "V"), huh.NewOption("Debug", "D"), huh.NewOption("Info", "I"), huh.NewOption("Warning", "W"), huh.NewOption("Error", "E"), huh.NewOption("Fatal", "F")).Value(&priority),
		huh.NewInput().Title("Package filter (optional; app must be running)").Placeholder("com.example.app").Value(&pkg),
	))
	if form.Run() != nil { return }
	ui.PrintWarning("Streaming Logcat for up to 5 minutes; press Ctrl+C to stop.")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute); defer cancel()
	if err := adb.StreamLogcat(ctx, session.Device.Serial, priority, strings.TrimSpace(pkg), os.Stdout); err != nil { ui.PrintError(err.Error()) }
	ui.Pause()
}

func installAPK(session *auth.Session) {
	path := pickLocalFile("Select APK to install")
	if path == "" { ui.PrintWarning("No APK selected."); return }
	if !strings.HasSuffix(strings.ToLower(path), ".apk") { ui.PrintError("Select a file ending in .apk"); ui.Pause(); return }
	var confirm string
	// Installation can replace an existing package; require a typed confirmation.
	fmt.Print("Type INSTALL to install this APK on "+session.Device.Serial+": ")
	if _, err := fmt.Scanln(&confirm); err != nil || confirm != "INSTALL" { ui.PrintWarning("Cancelled."); return }
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute); defer cancel()
	cmd := exec.CommandContext(ctx, "adb", "-s", session.Device.Serial, "install", "-r", path)
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if err := cmd.Run(); err != nil { ui.PrintError("APK installation failed: "+err.Error()) } else { ui.PrintSuccess("APK installation completed.") }
	ui.Pause()
}

func fileManager(session *auth.Session) {
	serial := session.Device.Serial
	current := "/sdcard"
	for {
		if !strings.HasPrefix(current, "/sdcard") || strings.ContainsAny(current, " \t\r\n;&|$<>\\\"'") {
			ui.PrintError("For safety, browse paths under /sdcard without shell-special characters.")
			current = "/sdcard"
		}
		out, err := adb.RunContext(context.Background(), serial, "shell", "ls", "-1Ap", current)
		if err != nil {
			ui.PrintError("Could not list " + current + ": " + err.Error())
			ui.Pause()
			return
		}
		options := []huh.Option[string]{
			huh.NewOption(".. (parent directory)", "__parent"),
		}
		for _, entry := range strings.Split(out, "\n") {
			entry = strings.TrimSpace(entry)
			if entry == "" || entry == "." || entry == ".." { continue }
			options = append(options, huh.NewOption(entry, entry))
		}
		options = append(options,
			huh.NewOption("Push a file from this computer into this directory", "__push"),
			huh.NewOption("Enter another /sdcard path", "__path"),
			huh.NewOption("Back to main menu", "__back"),
		)
		var selected string
		form := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("Device files: "+current).Options(options...).Value(&selected)))
		if err := form.Run(); err != nil || selected == "__back" { return }
		switch selected {
		case "__parent":
			parent := path.Dir(current)
			if parent == "/" || parent == "." { parent = "/sdcard" }
			current = parent
			continue
		case "__path":
			var next string
			form = huh.NewForm(huh.NewGroup(huh.NewInput().Title("Directory under /sdcard").Placeholder("/sdcard/Download").Value(&next)))
			if form.Run() != nil { continue }
			next = strings.TrimSpace(next)
			if !strings.HasPrefix(next, "/sdcard") || strings.ContainsAny(next, " \t\r\n;&|$<>\\\"'") {
				ui.PrintError("Enter a safe absolute path under /sdcard.")
				ui.Pause()
				continue
			}
			current = path.Clean(next)
			continue
		case "__push":
			var local string
			form = huh.NewForm(huh.NewGroup(huh.NewInput().Title("Local file path").Placeholder("/home/user/Downloads/file.zip").Value(&local)))
			if form.Run() != nil || strings.TrimSpace(local) == "" { continue }
			remote := path.Join(current, path.Base(strings.TrimSpace(local)))
			out, err := adb.Push(context.Background(), serial, strings.TrimSpace(local), remote)
			fmt.Println(out)
			if err != nil { ui.PrintError("Push failed: "+err.Error()) } else { ui.PrintSuccess("Pushed file to "+remote) }
			ui.Pause()
			continue
		}
		if strings.HasSuffix(selected, "/") {
			current = path.Join(current, strings.TrimSuffix(selected, "/"))
			continue
		}
		remote := path.Join(current, selected)
		var local string
		form = huh.NewForm(huh.NewGroup(huh.NewInput().Title("Save remote item to local path").Placeholder("./"+selected).Value(&local)))
		if form.Run() != nil || strings.TrimSpace(local) == "" { continue }
		out, err = adb.Pull(context.Background(), serial, remote, strings.TrimSpace(local))
		fmt.Println(out)
		if err != nil { ui.PrintError("Pull failed: "+err.Error()) } else { ui.PrintSuccess("Pulled "+remote+" to "+strings.TrimSpace(local)) }
		ui.Pause()
	}
}

func recordScreen(session *auth.Session) {
	var seconds int
	seconds = 60
	var duration string
	form := huh.NewForm(huh.NewGroup(huh.NewInput().Title("Recording duration in seconds (1–180)").Value(&duration).Placeholder("60")))
	if form.Run() == nil && duration != "" { if _, err := fmt.Sscanf(duration, "%d", &seconds); err != nil { ui.PrintError("Invalid duration"); return } }
	if seconds < 1 || seconds > 180 { ui.PrintError("Duration must be 1–180 seconds."); ui.Pause(); return }
	stamp := time.Now().Format("20060102-150405")
	remote := "/sdcard/adbgo-recording-"+stamp+".mp4"
	local := "adbgo-recording-"+stamp+".mp4"
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(seconds+30)*time.Second); defer cancel()
	cmd, err := adb.StartScreenRecording(ctx, session.Device.Serial, remote, seconds)
	if err != nil { ui.PrintError(err.Error()); ui.Pause(); return }
	ui.PrintSuccess(fmt.Sprintf("Recording started (maximum %d seconds). Wait for recording to finish…", seconds))
	if err := cmd.Wait(); err != nil { ui.PrintError("Recording command ended with an error: "+err.Error()); ui.Pause(); return }
	out, err := adb.Pull(ctx, session.Device.Serial, remote, local)
	fmt.Println(out); if err != nil { ui.PrintError(err.Error()) } else { ui.PrintSuccess("Recording saved to "+local) }
	_, _ = adb.Run(session.Device.Serial, "shell", "rm", "-f", remote)
	ui.Pause()
}

func multiDeviceTools() {
	devices, err := adb.ListDevices()
	if err != nil { ui.PrintError(err.Error()); ui.Pause(); return }
	var opts []huh.Option[string]
	for _, d := range devices { if d.Status == "device" { opts = append(opts, huh.NewOption(d.Serial+" ("+d.Model+")", d.Serial)) } }
	if len(opts) < 2 { ui.PrintWarning("Connect at least two authorized devices for multi-device mode."); ui.Pause(); return }
	var selected []string
	form := huh.NewForm(huh.NewGroup(huh.NewMultiSelect[string]().Title("Select devices").Options(opts...).Value(&selected)))
	if form.Run() != nil || len(selected) == 0 { return }
	var action string
	form = huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("Action for selected devices").Options(huh.NewOption("Read device model (safe)", "info"), huh.NewOption("Reboot selected devices", "reboot")).Value(&action)))
	if form.Run() != nil { return }
	if action == "reboot" {
		fmt.Printf("This will reboot %d selected device(s): %s\n", len(selected), strings.Join(selected, ", "))
		var confirm string
		fmt.Print("Type REBOOT to continue: ")
		if _, err := fmt.Scanln(&confirm); err != nil || confirm != "REBOOT" { ui.PrintWarning("Cancelled."); return }
	}
	for _, serial := range selected {
		if action == "reboot" {
			if err := adb.Reboot(serial, ""); err != nil { fmt.Printf("[%s] ERROR: %v\n", serial, err) } else { fmt.Printf("[%s] reboot command sent\n", serial) }
			continue
		}
		out, err := adb.RunContext(context.Background(), serial, "shell", "getprop", "ro.product.model")
		if err != nil { fmt.Printf("[%s] ERROR: %v\n", serial, err) } else { fmt.Printf("[%s] %s\n", serial, strings.TrimSpace(out)) }
	}
	ui.Pause()
}

func manageRegisteredDevices(cfg *config.Config, session *auth.Session) {
	for {
		if len(cfg.Devices) == 0 { ui.PrintWarning("No registered devices."); ui.Pause(); return }
		opts := make([]huh.Option[string], 0, len(cfg.Devices)+1)
		for _, d := range cfg.Devices { opts = append(opts, huh.NewOption(d.Name+" ("+d.Serial+")", d.Serial)) }
		opts = append(opts, huh.NewOption("Back", "back"))
		var serial string
		form := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("Registered devices").Options(opts...).Value(&serial)))
		if form.Run() != nil || serial == "back" { return }
		device := cfg.FindBySerial(serial); if device == nil { continue }
		var action string
		form = huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("Manage "+device.Name).Options(huh.NewOption("Rename", "rename"), huh.NewOption("Delete registration", "delete"), huh.NewOption("Back", "back")).Value(&action)))
		if form.Run() != nil || action == "back" { continue }
		if action == "rename" {
			var name string
			form = huh.NewForm(huh.NewGroup(huh.NewInput().Title("New friendly name").Value(&name)))
			if form.Run() != nil || strings.TrimSpace(name)=="" { continue }
			device.Name = strings.TrimSpace(name)
		} else {
			if serial == session.Device.Serial { ui.PrintError("Log out or switch devices before deleting the active session's registration."); ui.Pause(); continue }
			var yes bool
			form = huh.NewForm(huh.NewGroup(huh.NewConfirm().Title("Delete registration for "+serial+"? This does not modify the phone.").Value(&yes)))
			if form.Run() != nil || !yes { continue }
			cfg.RemoveDevice(serial)
		}
		if err := cfg.Save(); err != nil { ui.PrintError("Failed to save device registry: "+err.Error()) } else { ui.PrintSuccess("Device registry updated.") }
		ui.Pause()
	}
}
