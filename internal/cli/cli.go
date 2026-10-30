package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/dev-roba/adbgo/internal/adb"
	"github.com/dev-roba/adbgo/internal/ui"
	"github.com/dev-roba/adbgo/internal/update"
)

// PrintHelp displays CLI usage for direct command execution.
func PrintHelp() {
	fmt.Println(ui.TitleStyle.Render("adbgo — Powerful Terminal ADB Tool"))
	fmt.Println("Usage: adbgo [command] [options]")
	fmt.Println("\nWhen run without commands, adbgo opens the interactive TUI.")
	fmt.Println("\nCommands:")
	fmt.Println("  devices                     List all connected Android devices")
	fmt.Println("  scrcpy [-s serial]          Launch screen mirror via scrcpy")
	fmt.Println("  screenshot [-s serial]      Capture screen and save as PNG")
	fmt.Println("  record [-s serial]          Record screen to MP4 (Ctrl+C to stop)")
	fmt.Println("  install [-s serial] [-d] <file> Install APK or split bundle (.xapk/.apks)")
	fmt.Println("  logcat [-s serial] [-p pkg] Stream logcat filtered by package or priority")
	fmt.Println("  wireless connect [serial]   Enable wireless ADB (tcpip 5555) and connect")
	fmt.Println("  wireless pair <ip:port> <code> Pair with Android 11+ pairing code")
	fmt.Println("  wireless disconnect <ip:port> Disconnect a wireless device")
	fmt.Println("  shell [-s serial]           Open interactive adb shell")
	fmt.Println("  dashboard [-s serial]       Display live battery, storage, and display status")
	fmt.Println("  info [-s serial]            Display hardware & system properties")
	fmt.Println("  fingerprint [-s serial]      Security fingerprint & risk verdict")
	fmt.Println("  bruteforce [flags]           Lockscreen credential tester (pin/password/pattern)")
	fmt.Println("      -type pin|password|pattern -min N -max N -mask 08?? -wordlist file")
	fmt.Println("      -delay 500 -jitter 150 -max-attempts N -resume")
	fmt.Println("  update                      Check and install the latest adbgo update")
	fmt.Println("  version                     Print version info")
	fmt.Println()
}

// HandleCLI processes CLI arguments. Returns true if a command was executed,
// or false if the interactive TUI should run.
func HandleCLI(version string) bool {
	if len(os.Args) <= 1 {
		return false
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "-h", "--help", "help":
		PrintHelp()
		return true

	case "-v", "--version", "version":
		fmt.Printf("adbgo %s\n", version)
		return true

	case "devices":
		devices, err := adb.ListDevices()
		if err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		if len(devices) == 0 {
			ui.PrintWarning("No devices found.")
			return true
		}
		fmt.Printf("%-24s %-14s %-16s %s\n", "SERIAL", "STATUS", "MODEL", "PRODUCT")
		fmt.Println(strings.Repeat("─", 65))
		for _, d := range devices {
			fmt.Printf("%-24s %-14s %-16s %s\n", d.Serial, d.Status, d.Model, d.Product)
		}
		return true

	case "scrcpy":
		fs := flag.NewFlagSet("scrcpy", flag.ExitOnError)
		serial := fs.String("s", "", "Device serial")
		fs.Parse(args)
		target := getOrPickDevice(*serial)
		ui.PrintSuccess("Launching scrcpy for " + target)
		if err := adb.StartScrcpy(target); err != nil {
			ui.PrintError("scrcpy failed: " + err.Error())
			os.Exit(1)
		}
		return true

	case "screenshot":
		fs := flag.NewFlagSet("screenshot", flag.ExitOnError)
		serial := fs.String("s", "", "Device serial")
		fs.Parse(args)
		target := getOrPickDevice(*serial)
		file, err := adb.Screenshot(target)
		if err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		ui.PrintSuccess("Screenshot saved: " + file)
		return true

	case "record":
		fs := flag.NewFlagSet("record", flag.ExitOnError)
		serial := fs.String("s", "", "Device serial")
		fs.Parse(args)
		target := getOrPickDevice(*serial)
		file, err := adb.RecordScreen(target)
		if err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		ui.PrintSuccess("Recording saved: " + file)
		return true

	case "install":
		fs := flag.NewFlagSet("install", flag.ExitOnError)
		serial := fs.String("s", "", "Device serial")
		downgrade := fs.Bool("d", false, "Allow downgrade")
		fs.Parse(args)
		if fs.NArg() < 1 {
			ui.PrintError("Usage: adbgo install [-s serial] [-d] <path-to-apk>")
			os.Exit(1)
		}
		target := getOrPickDevice(*serial)
		pkgPath := fs.Arg(0)
		ui.PrintInfo(fmt.Sprintf("Installing %s to %s...", pkgPath, target))
		if err := adb.InstallPackage(target, pkgPath, *downgrade); err != nil {
			ui.PrintError("Installation failed: " + err.Error())
			os.Exit(1)
		}
		ui.PrintSuccess("Installed successfully!")
		return true

	case "wireless":
		handleWirelessCLI(args)
		return true

	case "logcat":
		fs := flag.NewFlagSet("logcat", flag.ExitOnError)
		serial := fs.String("s", "", "Device serial")
		pkg := fs.String("p", "", "Filter by package name")
		priority := fs.String("l", "", "Minimum priority (V, D, I, W, E, F)")
		crash := fs.Bool("crash", false, "Dump crash buffer only")
		fs.Parse(args)
		target := getOrPickDevice(*serial)
		if err := adb.RunLogcat(target, adb.LogcatFilter{
			Package:   *pkg,
			Priority:  *priority,
			DumpCrash: *crash,
		}); err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		return true

	case "shell":
		fs := flag.NewFlagSet("shell", flag.ExitOnError)
		serial := fs.String("s", "", "Device serial")
		fs.Parse(args)
		target := getOrPickDevice(*serial)
		if err := adb.RunStreaming(target, "shell"); err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		return true

	case "dashboard":
		fs := flag.NewFlagSet("dashboard", flag.ExitOnError)
		serial := fs.String("s", "", "Device serial")
		fs.Parse(args)
		target := getOrPickDevice(*serial)
		showDashboardCLI(target)
		return true

	case "info":
		fs := flag.NewFlagSet("info", flag.ExitOnError)
		serial := fs.String("s", "", "Device serial")
		fs.Parse(args)
		target := getOrPickDevice(*serial)
		out, err := adb.Run(target, "shell", "getprop")
		if err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.Contains(line, "ro.product.model") ||
				strings.Contains(line, "ro.build.version.release") ||
				strings.Contains(line, "ro.product.manufacturer") ||
				strings.Contains(line, "ro.build.version.sdk") ||
				strings.Contains(line, "ro.product.cpu.abi") {
				fmt.Println(line)
			}
		}
		return true

	case "fingerprint":
		fs := flag.NewFlagSet("fingerprint", flag.ExitOnError)
		serial := fs.String("s", "", "Device serial")
		jsonOut := fs.String("json", "", "Save fingerprint as JSON to this path")
		fs.Parse(args)
		target := getOrPickDevice(*serial)
		ui.PrintInfo("Profiling " + target + "...")
		fp, err := adb.CollectFingerprint(target)
		if err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		fmt.Println(fp.Report())
		fmt.Println("Verdict: " + fp.RiskVerdict())
		if *jsonOut != "" {
			if data, merr := json.MarshalIndent(fp, "", "  "); merr == nil {
				if werr := os.WriteFile(*jsonOut, data, 0o600); werr != nil {
					ui.PrintError("Could not save JSON: " + werr.Error())
				} else {
					ui.PrintSuccess("Saved: " + *jsonOut)
				}
			}
		}
		return true

	case "surface":
		fs := flag.NewFlagSet("surface", flag.ExitOnError)
		serial := fs.String("s", "", "Device serial")
		pkg := fs.String("pkg", "", "Package name to inspect")
		all := fs.Bool("all", false, "Scan all user apps")
		jsonOut := fs.String("json", "", "Save results as JSON to this path")
		fs.Parse(args)
		target := getOrPickDevice(*serial)
		if *all {
			ui.PrintInfo("Scanning all user apps on " + target + "...")
			surfaces, err := adb.ScanAllSurfaces(target, func(done, total int, pkg string) {
				fmt.Printf("\r  scanning %d/%d: %-45s", done+1, total, pkg)
			})
			fmt.Println()
			if err != nil {
				ui.PrintError(err.Error())
				os.Exit(1)
			}
			withIssues := 0
			for _, s := range surfaces {
				if len(s.Issues()) > 0 {
					withIssues++
					fmt.Println(s.Report())
				}
			}
			ui.PrintInfo(fmt.Sprintf("%d apps scanned, %d with findings", len(surfaces), withIssues))
			if *jsonOut != "" {
				if data, merr := json.MarshalIndent(surfaces, "", "  "); merr == nil {
					if werr := os.WriteFile(*jsonOut, data, 0o600); werr != nil {
						ui.PrintError("Could not save JSON: " + werr.Error())
					} else {
						ui.PrintSuccess("Saved: " + *jsonOut)
					}
				}
			}
			return true
		}
		if *pkg == "" {
			ui.PrintError("Provide -pkg com.example.app or -all")
			os.Exit(1)
		}
		s, err := adb.ScanAppSurface(target, *pkg)
		if err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		fmt.Println(s.Report())
		if *jsonOut != "" {
			if data, merr := json.MarshalIndent(s, "", "  "); merr == nil {
				if werr := os.WriteFile(*jsonOut, data, 0o600); werr != nil {
					ui.PrintError("Could not save JSON: " + werr.Error())
				} else {
					ui.PrintSuccess("Saved: " + *jsonOut)
				}
			}
		}
		return true

	case "bruteforce":
		fs := flag.NewFlagSet("bruteforce", flag.ExitOnError)
		serial := fs.String("s", "", "Device serial")
		typ := fs.String("type", "pin", "Credential type: pin, password or pattern")
		minLen := fs.Int("min", 4, "Minimum candidate length")
		maxLen := fs.Int("max", 4, "Maximum candidate length")
		charset := fs.String("charset", "", "Custom charset (default per type)")
		mask := fs.String("mask", "", "Mask with ? wildcards, e.g. 08??")
		wordlist := fs.String("wordlist", "", "Password mode: wordlist file")
		delayMs := fs.Int("delay", 500, "Delay between attempts in ms")
		jitterMs := fs.Int("jitter", 150, "Random extra delay in ms")
		maxAttempts := fs.Int("max-attempts", 0, "Stop after N attempts (0 = unlimited)")
		resume := fs.Bool("resume", false, "Resume previous session")
		fs.Parse(args)

		target := getOrPickDevice(*serial)
		bMin, bMax := *minLen, *maxLen
		if *typ == "pattern" && *minLen == 4 && *maxLen == 4 {
			bMin, bMax = 3, 9
		}

		ui.PrintWarning("Authorized use only — your own device, or one you have written permission to test.")
		ui.PrintWarning("If 'Erase data after 10 wrong attempts' is enabled, a failed run can FACTORY RESET the device.")
		ui.PrintInfo(fmt.Sprintf("Testing %s credential on %s (Ctrl+C saves the session and exits)", *typ, target))

		bcfg := adb.LockBruteConfig{
			Type:        *typ,
			MinLen:      bMin,
			MaxLen:      bMax,
			Charset:     *charset,
			Mask:        *mask,
			Wordlist:    *wordlist,
			CommonFirst: true,
			Delay:       time.Duration(*delayMs) * time.Millisecond,
			Jitter:      time.Duration(*jitterMs) * time.Millisecond,
			MaxAttempts: *maxAttempts,
			Resume:      *resume,
		}
		found, err := adb.RunLockBruteforce(target, bcfg, func(st adb.LockBruteStats) bool {
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
			os.Exit(1)
		}
		ui.PrintSuccess("CREDENTIAL FOUND: " + found)
		return true

	case "update":
		fmt.Println("Current version: " + version)
		latest, err := update.Latest()
		if err != nil {
			ui.PrintError("Could not check updates: " + err.Error())
			os.Exit(1)
		}
		fmt.Println("Latest release:  " + latest)
		if version != "dev" && !update.IsNewer(latest, version) {
			ui.PrintSuccess("adbgo is up to date!")
			return true
		}
		ui.PrintInfo("Installing update " + latest + "...")
		if err := update.Install(latest); err != nil {
			ui.PrintError("Update failed: " + err.Error())
			os.Exit(1)
		}
		ui.PrintSuccess("Updated successfully to " + latest + "! Restart adbgo to use the new version.")
		return true

	default:
		ui.PrintError("Unknown command: " + cmd)
		PrintHelp()
		os.Exit(1)
		return true
	}
}

func getOrPickDevice(specified string) string {
	if specified != "" {
		return specified
	}
	devices, err := adb.ListDevices()
	if err != nil || len(devices) == 0 {
		ui.PrintError("No online devices found. Connect via USB or Wi-Fi first.")
		os.Exit(1)
	}
	for _, d := range devices {
		if d.Status == "device" {
			return d.Serial
		}
	}
	ui.PrintError("No devices currently in 'device' status (check USB debugging authorization).")
	os.Exit(1)
	return ""
}

func handleWirelessCLI(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage:")
		fmt.Println("  adbgo wireless connect [serial]       Connect connected USB device over Wi-Fi")
		fmt.Println("  adbgo wireless pair <ip:port> <code>  Pair Android 11+ device with pairing code")
		fmt.Println("  adbgo wireless disconnect <ip:port>   Disconnect wireless device")
		fmt.Println("  adbgo wireless scan                   Scan for mDNS wireless targets")
		return
	}

	sub := args[0]
	switch sub {
	case "connect":
		var target string
		if len(args) > 1 {
			target = args[1]
		} else {
			target = getOrPickDevice("")
		}
		ui.PrintInfo("Enabling TCP/IP mode and connecting to " + target + "...")
		serial, err := adb.ConnectWireless(target)
		if err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		ui.PrintSuccess("Connected over Wi-Fi! Wireless serial: " + serial)

	case "pair":
		if len(args) < 3 {
			ui.PrintError("Usage: adbgo wireless pair <ip:port> <code>")
			os.Exit(1)
		}
		ipPort := args[1]
		code := args[2]
		ui.PrintInfo("Pairing with " + ipPort + "...")
		if err := adb.PairWireless(ipPort, code); err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		ui.PrintSuccess("Paired successfully! Now run: adbgo wireless connect " + ipPort)

	case "disconnect":
		if len(args) < 2 {
			ui.PrintError("Usage: adbgo wireless disconnect <ip:port>")
			os.Exit(1)
		}
		if err := adb.DisconnectWireless(args[1]); err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		ui.PrintSuccess("Disconnected " + args[1])

	case "scan":
		ui.PrintInfo("Scanning mDNS for wireless Android devices...")
		targets, err := adb.DiscoverMDNS()
		if err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		if len(targets) == 0 {
			ui.PrintWarning("No mDNS wireless devices found.")
			return
		}
		ui.PrintSuccess(fmt.Sprintf("Found %d wireless target(s):", len(targets)))
		for _, t := range targets {
			fmt.Println("  • " + t)
		}
	}
}

func showDashboardCLI(serial string) {
	fmt.Println(ui.TitleStyle.Render("Device Dashboard: " + serial))
	fmt.Println()

	// Battery
	batt, err := adb.GetBatteryInfo(serial)
	if err == nil {
		fmt.Printf("%s %d%%  |  %s  |  %.1f°C  |  %.2fV  |  Power: %s\n",
			ui.Highlight.Render("Battery:"),
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
		fmt.Printf("%s %s @ %s dpi\n", ui.Highlight.Render("Display:"), size, density)
	}

	// Active Window
	focused := adb.GetCurrentFocus(serial)
	fmt.Printf("%s %s\n", ui.Highlight.Render("Focused App:"), focused)

	// Storage
	storage, err := adb.GetStorageInfo(serial)
	if err == nil && len(storage) > 0 {
		fmt.Println("\n" + ui.Highlight.Render("Storage Usage:"))
		fmt.Printf("  %-12s %-8s %-8s %-8s %s\n", "PARTITION", "TOTAL", "USED", "FREE", "USE%")
		for _, s := range storage {
			fmt.Printf("  %-12s %-8s %-8s %-8s %s\n", s.MountedOn, s.Size, s.Used, s.Avail, s.UsePercent)
		}
	}
	fmt.Println()
}
