// cmd/banner.go
package cmd

import (
	"fmt"

	"charm.land/lipgloss/v2"
)

const asciiImmich = `
 ██╗███╗   ███╗███╗   ███╗██╗ ██████╗██╗  ██╗
 ██║████╗ ████║████╗ ████║██║██╔════╝██║  ██║
 ██║██╔████╔██║██╔████╔██║██║██║     ███████║
 ██║██║╚██╔╝██║██║╚██╔╝██║██║██║     ██╔══██║
 ██║██║ ╚═╝ ██║██║ ╚═╝ ██║██║╚██████╗██║  ██║
 ╚═╝╚═╝     ╚═╝╚═╝     ╚═╝╚═╝ ╚═════╝╚═╝  ╚═╝`

var (
	bannerArtStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#CBA6F7")).Bold(true)
	bannerWordStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#89B4FA")).Bold(true)
	bannerSubStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#6C7086"))
	bannerDotStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#45475A"))
)

func printBanner() {
	fmt.Println(bannerArtStyle.Render(asciiImmich))
	fmt.Println(
		bannerWordStyle.Render("  backup") +
			bannerDotStyle.Render("  ·  ") +
			bannerSubStyle.Render("rclone-powered backup for your Immich library"),
	)
	fmt.Println()
}
