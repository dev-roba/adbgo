package danger

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"adbgo/internal/adb"
	"adbgo/internal/ui"
)

// ConfirmYES forces the user to type exactly "YES"
func ConfirmYES(action string) bool {
	ui.PrintWarning("DANGEROUS ACTION: " + action)
	fmt.Print("Type YES to confirm: ")
	var input string
	fmt.Scanln(&input)
	return strings.TrimSpace(input) == "YES"
}

// ConfirmYESWithSerial requires YES + serial match
func ConfirmYESWithSerial(action, serial string) bool {
	ui.PrintWarning("EXTREME DANGER: " + action)
	fmt.Printf("This targets device: %s\n", serial)
	fmt.Print("Type YES to continue: ")
	var input string
	fmt.Scanln(&input)
	if strings.TrimSpace(input) != "YES" {
		return false
	}
	fmt.Print("Now type the device serial to double-confirm: ")
	var serialInput string
	fmt.Scanln(&serialInput)
	return strings.TrimSpace(serialInput) == serial
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
	return err
}

func WipeCache(serial string) error {
	if !ConfirmYES("Wipe Cache Partition") {
		ui.PrintError("Cancelled")
		return nil
	}
	_, err := adb.Run(serial, "shell", "recovery", "--wipe_cache")
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
	ui.PrintWarning("Device rebooting to bootloader... waiting a few seconds")
	cmd := exec.Command("fastboot", "-s", serial, "flashing", "unlock")
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
	cmd := exec.Command("fastboot", "-s", serial, "flashing", "lock")
	return cmd.Run()
}

func RemoveScreenLock(serial string) error {
	if !ConfirmYESWithSerial("REMOVE SCREEN LOCK (PIN/Pattern/Password)", serial) {
		ui.PrintError("Cancelled")
		return nil
	}
	commands := [][]string{
		{"shell", "locksettings", "clear", "--old", "0"},
		{"shell", "rm", "/data/system/gesture.key"},
		{"shell", "rm", "/data/system/password.key"},
		{"shell", "rm", "/data/system/locksettings.db*"},
	}
	for _, args := range commands {
		adb.Run(serial, args...)
	}
	ui.PrintSuccess("Attempted to remove screen lock. Reboot recommended.")
	return nil
}

func DisableFRP(serial string) error {
	if !ConfirmYESWithSerial("ATTEMPT FRP DISABLE", serial) {
		ui.PrintError("Cancelled")
		return nil
	}
	adb.Run(serial, "shell", "content", "insert", "--uri", "content://settings/secure", "--bind", "name:s:user_setup_complete", "--bind", "value:s:1")
	adb.Run(serial, "shell", "settings", "put", "global", "device_provisioned", "1")
	adb.Run(serial, "shell", "settings", "put", "secure", "user_setup_complete", "1")
	ui.PrintSuccess("FRP disable attempts sent.")
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

func FullRootShell(serial string) error {
	if !ConfirmYES("Open interactive root shell (su)") {
		ui.PrintError("Cancelled")
		return nil
	}
	cmd := exec.Command("adb", "-s", serial, "shell")
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	ui.PrintWarning("Starting shell. Type 'su' for root if available and authorized. Type 'exit' to return.")
	return cmd.Run()
}

func RawADB(serial string) error {
	fmt.Print("Enter raw adb arguments (without 'adb -s serial'): ")
	var line string
	fmt.Scanln(&line)
	if strings.TrimSpace(line) == "" {
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
	var line string
	fmt.Scanln(&line)
	args := strings.Fields(line)
	cmdArgs := append([]string{"-s", serial}, args...)
	cmd := exec.Command("fastboot", cmdArgs...)
	out, err := cmd.CombinedOutput()
	fmt.Println(string(out))
	return err
}
