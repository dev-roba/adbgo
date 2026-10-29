package danger

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/dev-roba/adbgo/internal/adb"
	"github.com/dev-roba/adbgo/internal/ui"
)

// ConfirmYES forces the user to type exactly "YES"
func ConfirmYES(action string) bool {
	ui.PrintWarning("DANGEROUS ACTION: " + action)
	fmt.Print("Type YES to confirm: ")
	return strings.TrimSpace(adb.ReadLine()) == "YES"
}

// ConfirmYESWithSerial requires YES + serial match
func ConfirmYESWithSerial(action, serial string) bool {
	ui.PrintWarning("EXTREME DANGER: " + action)
	fmt.Printf("This targets device: %s\n", serial)
	fmt.Print("Type YES to continue: ")
	if strings.TrimSpace(adb.ReadLine()) != "YES" {
		return false
	}
	fmt.Print("Now type the device serial to double-confirm: ")
	return strings.TrimSpace(adb.ReadLine()) == serial
}

func FactoryReset(serial string) error {
	if !ConfirmYESWithSerial("FACTORY RESET — ALL DATA WILL BE WIPED", serial) {
		ui.PrintError("Cancelled")
		return nil
	}
	ui.PrintWarning("Wiping device...")
	out, err := adb.Run(serial, "shell", "recovery", "--wipe_data")
	if err != nil {
		_, err = adb.Run(serial, "shell", "am", "broadcast", "-a", "android.intent.action.MASTER_CLEAR")
	}
	fmt.Println(out)
	if err != nil {
		ui.PrintError("Factory reset command failed. On most modern devices you need to reboot to recovery and wipe from there.")
	}
	return err
}

func WipeCache(serial string) error {
	if !ConfirmYES("Wipe Cache Partition") {
		ui.PrintError("Cancelled")
		return nil
	}
	_, err := adb.Run(serial, "shell", "recovery", "--wipe_cache")
	if err != nil {
		ui.PrintError("Wipe cache failed. On most devices this only works from recovery mode.")
	}
	return err
}

func UnlockBootloader(serial string) error {
	if !ConfirmYESWithSerial("UNLOCK BOOTLOADER — THIS WILL WIPE THE DEVICE", serial) {
		ui.PrintError("Cancelled")
		return nil
	}
	if err := adb.Reboot(serial, "bootloader"); err != nil {
		return err
	}
	ui.PrintWarning("Waiting for device to enter fastboot (up to 60s)...")
	if !adb.WaitForFastboot(serial, 60*time.Second) {
		ui.PrintError("Device did not show up in fastboot. Run 'fastboot devices' to check.")
		return nil
	}
	cmd := exec.Command("fastboot", "-s", serial, "flashing", "unlock")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func LockBootloader(serial string) error {
	if !ConfirmYESWithSerial("LOCK BOOTLOADER", serial) {
		ui.PrintError("Cancelled")
		return nil
	}
	if err := adb.Reboot(serial, "bootloader"); err != nil {
		return err
	}
	ui.PrintWarning("Waiting for device to enter fastboot (up to 60s)...")
	if !adb.WaitForFastboot(serial, 60*time.Second) {
		ui.PrintError("Device did not show up in fastboot. Run 'fastboot devices' to check.")
		return nil
	}
	cmd := exec.Command("fastboot", "-s", serial, "flashing", "lock")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}

func RemoveScreenLock(serial string) error {
	if !ConfirmYESWithSerial("REMOVE SCREEN LOCK (PIN/Pattern/Password)", serial) {
		ui.PrintError("Cancelled")
		return nil
	}
	versionStr, _ := adb.Run(serial, "shell", "getprop", "ro.build.version.release")
	versionStr = strings.TrimSpace(versionStr)
	ver, _ := strconv.Atoi(strings.Split(versionStr, ".")[0])
	if ver >= 9 {
		ui.PrintWarning(fmt.Sprintf("Device is running Android %s. Modern Android uses File-Based Encryption (FBE).", versionStr))
		ui.PrintWarning("Removing key files will NOT unlock the device and may brick the keystore.")
		if !ConfirmYES("Proceed anyway despite risk of bricking keystore") {
			ui.PrintError("Aborted for safety.")
			return nil
		}
	} else {
		ui.PrintWarning("Note: this only works on rooted devices or legacy Android versions.")
	}

	commands := [][]string{
		{"shell", "locksettings", "clear", "--old", ""},
		{"shell", "su", "-c", "rm", "-f", "/data/system/gesture.key"},
		{"shell", "su", "-c", "rm", "-f", "/data/system/password.key"},
		{"shell", "su", "-c", "rm", "-f", "/data/system/locksettings.db*"},
	}
	for _, args := range commands {
		adb.Run(serial, args...)
	}
	ui.PrintSuccess("Screen lock removal attempted. Reboot recommended.")
	return nil
}

func DisableFRP(serial string) error {
	if !ConfirmYESWithSerial("ATTEMPT FRP DISABLE", serial) {
		ui.PrintError("Cancelled")
		return nil
	}
	ui.PrintWarning("Attempting standard provisioning flags. (Note: patched on Android 8+).")
	adb.Run(serial, "shell", "content", "insert", "--uri", "content://settings/secure", "--bind", "name:s:user_setup_complete", "--bind", "value:s:1")
	adb.Run(serial, "shell", "settings", "put", "global", "device_provisioned", "1")
	adb.Run(serial, "shell", "settings", "put", "secure", "user_setup_complete", "1")
	ui.PrintSuccess("FRP disable broadcast commands sent.")
	return nil
}

func FormatData(serial string) error {
	if !ConfirmYESWithSerial("FORMAT /data — ALL USER DATA WILL BE DESTROYED", serial) {
		ui.PrintError("Cancelled")
		return nil
	}
	_, err := adb.Run(serial, "shell", "su", "-c", "rm", "-rf", "/data/*")
	return err
}

// FullRootShell fixes issue #2: it tests if adbd can restart as root; if not,
// it launches an interactive root shell forcing PTY allocation (-t) with su
// so stdin, arrows, and terminal control work properly.
func FullRootShell(serial string) error {
	if !ConfirmYES("Open interactive root shell") {
		ui.PrintError("Cancelled")
		return nil
	}

	ui.PrintInfo("Checking if adbd can run as root...")
	out, _ := adb.Run(serial, "root")
	if strings.Contains(out, "restarting adbd as root") {
		ui.PrintSuccess("adbd restarted as root! Spawning root shell...")
		time.Sleep(1 * time.Second)
		return adb.RunStreaming(serial, "shell")
	}
	if strings.Contains(out, "already running as root") {
		ui.PrintSuccess("adbd already running as root! Spawning root shell...")
		return adb.RunStreaming(serial, "shell")
	}

	ui.PrintInfo("adbd cannot run as root directly. Allocating PTY for 'su'...")
	// Force PTY (-t) so stdin and terminal interactive input work with su!
	return adb.RunStreaming(serial, "shell", "-t", "su")
}

func RawADB(serial string) error {
	fmt.Print("Enter raw adb arguments (without 'adb -s serial'): ")
	line := adb.ReadLine()
	if line == "" {
		return nil
	}
	args := strings.Fields(line)
	out, err := adb.Run(serial, args...)
	fmt.Println(out)
	return err
}

func RawFastboot(serial string) error {
	if !ConfirmYES("Raw fastboot command (device should be in fastboot mode)") {
		return nil
	}
	fmt.Print("Enter fastboot arguments: ")
	line := adb.ReadLine()
	if line == "" {
		return nil
	}
	cmdArgs := append([]string{"-s", serial}, strings.Fields(line)...)
	cmd := exec.Command("fastboot", cmdArgs...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	return cmd.Run()
}
