package danger

import (
	"bufio"
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
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(line)
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
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" { return nil }
	args := strings.Fields(line)
	cmdArgs := append([]string{"-s", serial}, args...)
	cmd := exec.Command("fastboot", cmdArgs...)
	out, err := cmd.CombinedOutput()
	fmt.Println(string(out))
	return err
}
