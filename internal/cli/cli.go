package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/dev-roba/adbgo/internal/adb"
	"github.com/dev-roba/adbgo/internal/qr"
	"github.com/dev-roba/adbgo/internal/relay"
	"github.com/dev-roba/adbgo/internal/share"
	"github.com/dev-roba/adbgo/internal/ui"
	"github.com/dev-roba/adbgo/internal/update"
	"github.com/dev-roba/adbgo/internal/webpanel"
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
	fmt.Println("  wireless share [wan]         Share a link/QR that connects a phone by itself")
	fmt.Println("  shell [-s serial]           Open interactive adb shell")
	fmt.Println("  dashboard [-s serial]       Display live battery, storage, and display status")
	fmt.Println("  info [-s serial]            Display hardware & system properties")
	fmt.Println("  photos recent [-n N] [-json]   List newest photos (date, size, path)")
	fmt.Println("  photos pull [-dir DIR] [-dest F] [-move -yes] Copy or cut media")
	fmt.Println("  apps [-json]                   List installed third-party packages")
	fmt.Println("  fingerprint [-s serial]      Security fingerprint & risk verdict")
	fmt.Println("  notifications [-s serial] [-json]  Mirror phone notifications")
	fmt.Println("  findmyphone [-s serial]       Max volume + alarm to locate the phone")
	fmt.Println("  triage <pkg> [-s serial] [-json]  Pull APK & static analysis")
	fmt.Println("  bruteforce [flags]           Lockscreen credential tester (pin/password/pattern)")
	fmt.Println("      -type pin|password|pattern -min N -max N -mask 08?? -wordlist file")
	fmt.Println("      -delay 500 -jitter 150 -max-attempts N -resume")
	fmt.Println("  connect [name]              Smart connect: USB → mDNS → saved → LAN sweep")
	fmt.Println("  pairqr                      QR pairing: the phone scans, adbgo pairs")
	fmt.Println("  watch [-s serial|target]    Watchdog: auto-reconnect + desktop alerts")
	fmt.Println("  radar                       Auto-connect phones as they join Wi-Fi")
	fmt.Println("  recon                       Sweep your LAN for port 5555 (own network)")
	fmt.Println("  web [-s serial] [-wan]      Browser control panel (tap, type, keys)")
	fmt.Println("  tunnel up|down|list|gen-frp  SSH reverse tunnel manager")
	fmt.Println("  relay serve|expose|connect  Token-gated relay between two machines")
	fmt.Println("  exec [-s serial] [--all] \"cmd\"  Run a shell command on device(s)")
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

	case "photos":
		if len(args) == 0 {
			fmt.Println("Usage:")
			fmt.Println("  adbgo photos recent [-s serial] [-n N] [-json]      List the N newest photos")
			fmt.Println("  adbgo photos pull [-s serial] [-dir DIR] [-dest F] [-move] [-yes]")
			return true
		}
		switch args[0] {
		case "recent":
			fs := flag.NewFlagSet("photos recent", flag.ExitOnError)
			serial := fs.String("s", "", "Device serial")
			n := fs.Int("n", 30, "How many recent files to list")
			jsonOut := fs.Bool("json", false, "Print as JSON")
			fs.Parse(args[1:])
			target := getOrPickDevice(*serial)
			ui.PrintInfo("Finding recent media on " + target + "...")
			infos := adb.RecentMedia(target, []string{"/sdcard/DCIM/Camera", "/sdcard/Pictures/Screenshots", "/sdcard/Movies/ScreenRecords", "/sdcard/Pictures"}, *n)
			if len(infos) == 0 {
				ui.PrintWarning("No media files found.")
				return true
			}
			if *jsonOut {
				data, merr := json.MarshalIndent(infos, "", "  ")
				if merr != nil {
					ui.PrintError(merr.Error())
					os.Exit(1)
				}
				fmt.Println(string(data))
				return true
			}
			fmt.Printf("%-40s %-9s %-17s %s\n", "NAME", "SIZE", "MODIFIED", "PATH")
			fmt.Println(strings.Repeat("─", 100))
			for _, f := range infos {
				fmt.Printf("%-40s %-9s %-17s %s\n", f.Name, adb.HumanSize(f.Size), time.Unix(f.Mtime, 0).Format("2006-01-02 15:04"), f.Path)
			}
			ui.PrintInfo(fmt.Sprintf("%d file(s). Pull with: adbgo photos pull -dir /sdcard/DCIM/Camera", len(infos)))

		case "pull":
			fs := flag.NewFlagSet("photos pull", flag.ExitOnError)
			serial := fs.String("s", "", "Device serial")
			dir := fs.String("dir", "/sdcard/DCIM/Camera", "Remote directory to pull from")
			dest := fs.String("dest", "adbgo-photos", "Destination folder on PC")
			recentN := fs.Int("recent", 0, "Only the N newest files (0 = all)")
			move := fs.Bool("move", false, "Cut: delete from phone after transfer")
			yes := fs.Bool("yes", false, "Required with -move to skip the safety prompt")
			fs.Parse(args[1:])
			target := getOrPickDevice(*serial)
			if *move && !*yes {
				ui.PrintError("Cut mode deletes photos from the phone. Re-run with -move -yes to confirm you accept this.")
				os.Exit(1)
			}
			ui.PrintInfo("Finding media in " + *dir + "...")
			infos, err := adb.ListRemoteMediaInfo(target, *dir)
			if err != nil {
				ui.PrintError(err.Error())
				os.Exit(1)
			}
			if *recentN > 0 && len(infos) > *recentN {
				sorted := make([]adb.MediaInfo, len(infos))
				copy(sorted, infos)
				sort.Slice(sorted, func(i, j int) bool { return sorted[i].Mtime > sorted[j].Mtime })
				infos = sorted[:*recentN]
			}
			if len(infos) == 0 {
				ui.PrintWarning("No media files found in " + *dir)
				return true
			}
			ui.PrintInfo(fmt.Sprintf("Transferring %d file(s) to %s...", len(infos), *dest))
			pulled, moved, failed, err := adb.PullFiles(target, infos, *dir, *dest, *move, func(done, total int, name string) {
				fmt.Printf("\r  transferring %d/%d: %-45s", done, total, name)
			})
			fmt.Println()
			if err != nil {
				ui.PrintError(err.Error())
				os.Exit(1)
			}
			ui.PrintSuccess(fmt.Sprintf("Copied %d file(s) to %s", pulled, *dest))
			if *move {
				ui.PrintSuccess(fmt.Sprintf("Deleted %d file(s) from the phone (cut)", moved))
			}
			if failed > 0 {
				ui.PrintWarning(fmt.Sprintf("%d file(s) had problems — anything not fully transferred was NOT deleted from the phone.", failed))
			}

		default:
			ui.PrintError("Unknown photos subcommand: " + args[0])
			fmt.Println("Try: adbgo photos recent | adbgo photos pull")
			os.Exit(1)
		}
		return true

	case "apps":
		fs := flag.NewFlagSet("apps", flag.ExitOnError)
		serial := fs.String("s", "", "Device serial")
		all := fs.Bool("a", false, "Include system packages too")
		jsonOut := fs.Bool("json", false, "Print as JSON")
		fs.Parse(args)
		target := getOrPickDevice(*serial)
		shellArgs := []string{"shell", "pm", "list", "packages", "-3"}
		if *all {
			shellArgs = []string{"shell", "pm", "list", "packages"}
		}
		out, err := adb.Run(target, shellArgs...)
		if err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		var pkgs []string
		for _, line := range strings.Split(out, "\n") {
			p := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "package:"))
			if p != "" {
				pkgs = append(pkgs, p)
			}
		}
		sort.Strings(pkgs)
		if *jsonOut {
			data, merr := json.MarshalIndent(pkgs, "", "  ")
			if merr != nil {
				ui.PrintError(merr.Error())
				os.Exit(1)
			}
			fmt.Println(string(data))
			return true
		}
		for _, p := range pkgs {
			fmt.Println(p)
		}
		ui.PrintInfo(fmt.Sprintf("%d package(s).", len(pkgs)))
		return true

	case "notifications":
		fs := flag.NewFlagSet("notifications", flag.ExitOnError)
		serial := fs.String("s", "", "Device serial")
		jsonOut := fs.Bool("json", false, "Print as JSON")
		fs.Parse(args)
		target := getOrPickDevice(*serial)
		ui.PrintInfo("Fetching notifications...")
		notifs, err := adb.ListNotifications(target)
		if err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		if *jsonOut {
			data, merr := json.MarshalIndent(notifs, "", "  ")
			if merr != nil {
				ui.PrintError(merr.Error())
				os.Exit(1)
			}
			fmt.Println(string(data))
			return true
		}
		fmt.Printf("%-28s %-30s %s\n", "APP", "TITLE", "TEXT")
		fmt.Println(strings.Repeat("─", 92))
		for _, n := range notifs {
			fmt.Printf("%-28s %-30s %s\n", n.Package, n.Title, n.Text)
		}
		return true

	case "findmyphone":
		fs := flag.NewFlagSet("findmyphone", flag.ExitOnError)
		serial := fs.String("s", "", "Device serial")
		fs.Parse(args)
		target := getOrPickDevice(*serial)
		ui.PrintInfo("Ringing " + target + "...")
		if err := adb.FindMyPhone(target); err != nil {
			ui.PrintWarning(err.Error())
			return true
		}
		ui.PrintSuccess("🔊 Phone should be ringing!")
		return true

	case "triage":
		if len(args) == 0 {
			ui.PrintError("Usage: adbgo triage <package> [-s serial] [-json]")
			os.Exit(1)
		}
		fs := flag.NewFlagSet("triage", flag.ExitOnError)
		serial := fs.String("s", "", "Device serial")
		jsonOut := fs.Bool("json", false, "Print as JSON")
		fs.Parse(args)
		pkg := fs.Arg(0)
		target := getOrPickDevice(*serial)
		dest := filepath.Join(os.TempDir(), "adbgo-apk")
		os.MkdirAll(dest, 0o755)
		ui.PrintInfo("Pulling APK(s) for " + pkg + "...")
		files, err := adb.ExtractAPK(target, pkg, dest)
		if err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		if len(files) == 0 {
			ui.PrintError("No APK files found.")
			return true
		}
		ui.PrintSuccess(fmt.Sprintf("Pulled %d APK file(s). Analyzing...", len(files)))
		report, terr := adb.TriageAPK(files[0], pkg)
		if terr != nil {
			ui.PrintError("Triage failed: " + terr.Error())
			os.Exit(1)
		}
		if *jsonOut {
			data, merr := json.MarshalIndent(report, "", "  ")
			if merr != nil {
				ui.PrintError(merr.Error())
				os.Exit(1)
			}
			fmt.Println(string(data))
			return true
		}
		fmt.Println()
		fmt.Println(report.Report())
		return true

	case "connect":
		name := ""
		if len(args) > 0 {
			name = args[0]
		}
		ui.PrintInfo("Connect chain: USB → mDNS → saved → LAN sweep...")
		serial, how, err := adb.SmartConnect(name, func(msg string) { ui.PrintInfo(msg) })
		if err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		ui.PrintSuccess("Connected via " + how + " → " + serial)
		return true

	case "pairqr":
		payload, name, code := adb.GeneratePairQR()
		mat, qerr := qr.Encode(payload)
		fmt.Println("On the phone: Settings → Developer options → Wireless debugging → Pair device with QR code")
		fmt.Println("Service: " + name + "    Code: " + code)
		if qerr == nil {
			fmt.Println(qr.RenderANSI(mat))
		}
		ui.PrintInfo("Waiting for the phone over mDNS (up to 120s)...")
		addr, werr := adb.WaitForPairingService(name, 120*time.Second, nil)
		if werr != nil {
			ui.PrintError(werr.Error())
			os.Exit(1)
		}
		out, perr := adb.PairWithAddr(addr, code)
		if perr != nil {
			ui.PrintError("Pairing failed: " + perr.Error() + " " + strings.TrimSpace(out))
			os.Exit(1)
		}
		ui.PrintSuccess("Paired: " + strings.TrimSpace(out))
		return true

	case "watch":
		fs := flag.NewFlagSet("watch", flag.ExitOnError)
		serial := fs.String("s", "", "Device serial or ip:port to watch")
		fs.Parse(args)
		target := *serial
		if target == "" && fs.NArg() > 0 {
			target = fs.Arg(0)
		}
		ui.PrintInfo("Watchdog running — Ctrl+C to stop. Desktop alerts on connect/disconnect.")
		adb.Watchdog(nil, target, func(event, msg string) {
			if event == "connect" {
				ui.PrintSuccess("📱 connected (" + msg + ")")
			} else {
				ui.PrintWarning("📵 disconnected — did you forget it somewhere?")
			}
		})
		return true

	case "radar":
		ui.PrintInfo("Radar running — Ctrl+C to stop. Phones auto-connect when they join the Wi-Fi.")
		adb.Radar(nil, func(msg string) { ui.PrintSuccess(msg) })
		return true

	case "recon":
		ui.PrintWarning("Authorized use only — scan networks you own.")
		ui.PrintInfo("Sweeping local subnets for port 5555 (takes ~10s)...")
		targets, err := adb.ScanLANPorts(5555, func(done, total int) {
			fmt.Printf("\r  probing %d/%d hosts...", done, total)
		})
		fmt.Println()
		if err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		if len(targets) == 0 {
			ui.PrintWarning("No devices with port 5555 open.")
			return true
		}
		for _, t := range targets {
			fmt.Println("  • " + t)
		}
		ui.PrintInfo("Connect one with: adbgo connect <ip:5555>")
		return true

	case "web":
		fs := flag.NewFlagSet("web", flag.ExitOnError)
		serial := fs.String("s", "", "Device serial")
		wan := fs.Bool("wan", false, "Expose over the internet via pinggy")
		fs.Parse(args)
		target := getOrPickDevice(*serial)
		mode := "lan"
		if *wan {
			mode = "wan"
		}
		webpanel.Run(target, mode)
		return true

	case "tunnel":
		handleTunnelCLI(args)
		return true

	case "relay":
		handleRelayCLI(args)
		return true

	case "exec":
		fs := flag.NewFlagSet("exec", flag.ExitOnError)
		serial := fs.String("s", "", "Device serial")
		all := fs.Bool("all", false, "Run on ALL connected devices")
		fs.Parse(args)
		if fs.NArg() < 1 {
			ui.PrintError("Usage: adbgo exec [-s serial] [--all] \"shell command\"")
			os.Exit(1)
		}
		command := fs.Arg(0)
		if *all {
			devices, derr := adb.ListDevices()
			if derr != nil {
				ui.PrintError(derr.Error())
				os.Exit(1)
			}
			ran := false
			for _, d := range devices {
				if d.Status != "device" {
					continue
				}
				ran = true
				out, cerr := adb.Run(d.Serial, "shell", command)
				status := "✓"
				if cerr != nil {
					status = "✗"
				}
				fmt.Println(ui.Highlight.Render(d.Serial) + "  " + status)
				fmt.Println(strings.TrimSpace(out))
				fmt.Println()
			}
			if !ran {
				ui.PrintWarning("No online devices.")
			}
			return true
		}
		target := getOrPickDevice(*serial)
		out, cerr := adb.Run(target, "shell", command)
		if cerr != nil {
			ui.PrintError(cerr.Error())
		}
		fmt.Println(out)
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
	fmt.Println("  adbgo wireless share [wan]             Share a link/QR (LAN, or wan for an internet tunnel)")
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

	case "share":
		mode := "lan"
		if len(args) > 1 && args[1] == "wan" {
			mode = "wan"
		}
		share.Run(mode)

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

// handleTunnelCLI implements the `adbgo tunnel` subcommands.
func handleTunnelCLI(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage:")
		fmt.Println("  adbgo tunnel up <name> -via user@vps -local 8080 -remote 8080")
		fmt.Println("  adbgo tunnel down <name>")
		fmt.Println("  adbgo tunnel list")
		fmt.Println("  adbgo tunnel gen-frp <name> -server vps.example.com -local 5555 -remote 15555")
		return
	}
	switch args[0] {
	case "up":
		fs := flag.NewFlagSet("tunnel up", flag.ExitOnError)
		via := fs.String("via", "", "SSH target user@vps")
		local := fs.Int("local", 0, "Local port to expose")
		remote := fs.Int("remote", 0, "Remote port on the VPS")
		fs.Parse(args[1:])
		if fs.NArg() < 1 || *via == "" || *local <= 0 || *remote <= 0 {
			ui.PrintError("Usage: adbgo tunnel up <name> -via user@vps -local 8080 -remote 8080")
			os.Exit(1)
		}
		var t adb.Tunnel
		t.Name = fs.Arg(0)
		t.Via = *via
		t.LocalPort = *local
		t.RemotePort = *remote
		t.Created = time.Now().Format("2006-01-02 15:04")
		ui.PrintInfo("Tunnel up — Ctrl+C to stop (auto-heals if the connection drops).")
		if err := adb.TunnelUp(t); err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}

	case "down":
		if len(args) < 2 {
			ui.PrintError("Usage: adbgo tunnel down <name>")
			os.Exit(1)
		}
		adb.TunnelDown(args[1])
		ui.PrintSuccess("Stopped " + args[1])

	case "list":
		tunnels := adb.LoadTunnels()
		if len(tunnels) == 0 {
			ui.PrintWarning("No saved tunnels.")
			return
		}
		for _, t := range tunnels {
			status := "stopped"
			if adb.TunnelRunning(t.Name) {
				status = "running"
			}
			fmt.Printf("  %-12s %-24s localhost:%-5d → remote:%d  [%s]\n", t.Name, t.Via, t.LocalPort, t.RemotePort, status)
		}

	case "gen-frp":
		fs := flag.NewFlagSet("tunnel gen-frp", flag.ExitOnError)
		server := fs.String("server", "", "frps server address")
		local := fs.Int("local", 0, "Local port")
		remote := fs.Int("remote", 0, "Remote port on the frps server")
		fs.Parse(args[1:])
		if fs.NArg() < 1 || *server == "" || *local <= 0 || *remote <= 0 {
			ui.PrintError("Usage: adbgo tunnel gen-frp <name> -server vps.example.com -local 5555 -remote 15555")
			os.Exit(1)
		}
		path, err := adb.GenFRPConfig(fs.Arg(0), *server, *local, *remote)
		if err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}
		ui.PrintSuccess("frp config written: " + path)
		ui.PrintInfo("Run it with: frpc -c " + path)

	default:
		ui.PrintError("Unknown tunnel subcommand: " + args[0])
		os.Exit(1)
	}
}

// handleRelayCLI implements the `adbgo relay` subcommands.
func handleRelayCLI(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage:")
		fmt.Println("  adbgo relay serve -port 17000 -token SECRET")
		fmt.Println("  adbgo relay expose -host vps:17000 -token SECRET -service localhost:5555")
		fmt.Println("  adbgo relay connect -host vps:17000 -token SECRET -listen 5556")
		return
	}
	switch args[0] {
	case "serve":
		fs := flag.NewFlagSet("relay serve", flag.ExitOnError)
		port := fs.String("port", "17000", "Listen port")
		token := fs.String("token", "", "Shared secret token")
		fs.Parse(args[1:])
		if *token == "" {
			ui.PrintError("A -token is required")
			os.Exit(1)
		}
		if err := relay.Serve(*port, *token); err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}

	case "expose":
		fs := flag.NewFlagSet("relay expose", flag.ExitOnError)
		host := fs.String("host", "", "Relay host (vps:17000)")
		token := fs.String("token", "", "Shared secret token")
		service := fs.String("service", "localhost:5555", "Local TCP service to expose")
		fs.Parse(args[1:])
		if *host == "" || *token == "" {
			ui.PrintError("-host and -token are required")
			os.Exit(1)
		}
		if err := relay.Expose(*host, *token, *service); err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}

	case "connect":
		fs := flag.NewFlagSet("relay connect", flag.ExitOnError)
		host := fs.String("host", "", "Relay host (vps:17000)")
		token := fs.String("token", "", "Shared secret token")
		listen := fs.Int("listen", 5556, "Local port to serve the tunnel on")
		fs.Parse(args[1:])
		if *host == "" || *token == "" {
			ui.PrintError("-host and -token are required")
			os.Exit(1)
		}
		if err := relay.Connect(*host, *token, *listen); err != nil {
			ui.PrintError(err.Error())
			os.Exit(1)
		}

	default:
		ui.PrintError("Unknown relay subcommand: " + args[0])
		os.Exit(1)
	}
}
