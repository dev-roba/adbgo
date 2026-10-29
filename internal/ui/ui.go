package ui

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ReadLine reads a full line from stdin (handles spaces, unlike fmt.Scanln).
func ReadLine() string {
	r := bufio.NewReader(os.Stdin)
	line, _ := r.ReadString('\n')
	return strings.TrimSpace(line)
}

var (
	// Colors
	Green  = lipgloss.Color("#00FF87")
	Red    = lipgloss.Color("#FF005F")
	Cyan   = lipgloss.Color("#00D7FF")
	Yellow = lipgloss.Color("#FFD700")
	Gray   = lipgloss.Color("#6C6C6C")
	White  = lipgloss.Color("#FFFFFF")

	// Styles
	TitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Cyan).
			MarginBottom(1)

	NormalBanner = lipgloss.NewStyle().
			Bold(true).
			Foreground(Green).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(Green).
			Padding(0, 2)

	GodBanner = lipgloss.NewStyle().
			Bold(true).
			Foreground(Red).
			Border(lipgloss.ThickBorder()).
			BorderForeground(Red).
			Padding(0, 2)

	WarningStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(Red)

	SuccessStyle = lipgloss.NewStyle().
			Foreground(Green)

	ErrorStyle = lipgloss.NewStyle().
			Foreground(Red)

	DimStyle = lipgloss.NewStyle().
			Foreground(Gray)

	Highlight = lipgloss.NewStyle().
			Foreground(Yellow).
			Bold(true)
)

func ClearScreen() {
	fmt.Print("\033[H\033[2J")
}

func PrintBanner(godMode bool, deviceName, serial string) {
	ClearScreen()

	if godMode {
		banner := GodBanner.Render("⚠  GOD MODE ACTIVE  ⚠")
		fmt.Println(banner)
		fmt.Println(WarningStyle.Render("  NUCLEAR OPTIONS UNLOCKED — BE CAREFUL"))
	} else {
		banner := NormalBanner.Render("adbgo — ADB Power Tool")
		fmt.Println(banner)
	}

	fmt.Println()
	fmt.Printf("%s  %s\n", DimStyle.Render("Device:"), Highlight.Render(deviceName))
	fmt.Printf("%s  %s\n", DimStyle.Render("Serial:"), DimStyle.Render(serial))
	fmt.Println(strings.Repeat("─", 50))
	fmt.Println()
}

func PrintSuccess(msg string) {
	fmt.Println(SuccessStyle.Render("✓ " + msg))
}

func PrintError(msg string) {
	fmt.Println(ErrorStyle.Render("✗ " + msg))
}

func PrintWarning(msg string) {
	fmt.Println(WarningStyle.Render("⚠ " + msg))
}

func PrintInfo(msg string) {
	fmt.Println(DimStyle.Render("  " + msg))
}

func Pause() {
	fmt.Print(DimStyle.Render("\nPress Enter to continue..."))
	ReadLine()
}
