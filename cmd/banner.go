// cmd/banner.go
package cmd

import (
	"fmt"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
)

// Version is injected at build time via ldflags:
//
//	-X github.com/daksh7011/immich-backup/cmd.Version=<tag>
var Version = "dev"

// asciiArt spells out "IMMICH BACKUP" in ANSI Shadow block font.
const asciiArt = ` ██╗███╗   ███╗███╗   ███╗██╗ ██████╗██╗  ██╗    ██████╗  █████╗  ██████╗██╗  ██╗██╗   ██╗██████╗
 ██║████╗ ████║████╗ ████║██║██╔════╝██║  ██║    ██╔══██╗██╔══██╗██╔════╝██║ ██╔╝██║   ██║██╔══██╗
 ██║██╔████╔██║██╔████╔██║██║██║     ███████║    ██████╔╝███████║██║     █████╔╝ ██║   ██║██████╔╝
 ██║██║╚██╔╝██║██║╚██╔╝██║██║██║     ██╔══██║    ██╔══██╗██╔══██║██║     ██╔═██╗ ██║   ██║██╔═══╝
 ██║██║ ╚═╝ ██║██║ ╚═╝ ██║██║╚██████╗██║  ██║    ██████╔╝██║  ██║╚██████╗██║  ██╗╚██████╔╝██║
 ╚═╝╚═╝     ╚═╝╚═╝     ╚═╝╚═╝ ╚═════╝╚═╝  ╚═╝    ╚═════╝ ╚═╝  ╚═╝ ╚═════╝╚═╝  ╚═╝ ╚═════╝ ╚═╝`

var (
	artStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#CBA6F7")).Bold(true)
	nameStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#CBA6F7")).Bold(true)
	versionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#89B4FA")).Bold(true)
	subStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#6C7086"))
	dirStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#A6ADC8"))
	dotStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#45475A"))
)

func versionLabel() string {
	if Version == "dev" {
		return "dev"
	}
	return "v" + strings.TrimPrefix(Version, "v")
}

func currentDir() string {
	cwd, err := os.Getwd()
	if err != nil {
		return "."
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return cwd
	}
	if strings.HasPrefix(cwd, home) {
		return "~" + cwd[len(home):]
	}
	return cwd
}

func printBanner() {
	dot := dotStyle.Render("  ·  ")

	fmt.Println()
	fmt.Println(artStyle.Render(asciiArt))
	fmt.Println()
	fmt.Println("  " + nameStyle.Render("immich-backup") + dot + versionStyle.Render(versionLabel()) + dot + dirStyle.Render(currentDir()))
	fmt.Println("  " + subStyle.Render("rclone-powered backup for your Immich library"))
	fmt.Println()
}
