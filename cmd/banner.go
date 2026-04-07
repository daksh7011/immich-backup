// cmd/banner.go
package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"charm.land/lipgloss/v2"
)

// Version is injected at build time via ldflags:
//
//	-X github.com/daksh7011/immich-backup/cmd.Version=<tag>
var Version = "dev"

// logo is a compact 3-line block-art mark for immich-backup.
const logo = " ╔══════╗\n ║  ib  ║\n ╚══════╝"

var (
	logoStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#CBA6F7")).Bold(true)
	nameStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#CBA6F7")).Bold(true)
	versionStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#89B4FA"))
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
	logoLines := strings.Split(logo, "\n")
	dot := dotStyle.Render(" · ")

	infoLines := []string{
		nameStyle.Render("immich-backup") + dot + versionStyle.Render(versionLabel()),
		subStyle.Render("rclone-powered backup for your Immich library"),
		dirStyle.Render(filepath.ToSlash(currentDir())),
	}

	fmt.Println()
	for i, line := range logoLines {
		left := logoStyle.Render(line)
		if i < len(infoLines) {
			fmt.Printf("%s   %s\n", left, infoLines[i])
		} else {
			fmt.Println(left)
		}
	}
	fmt.Println()
}
