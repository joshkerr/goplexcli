package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// Logo prints a styled ASCII art logo with version information
func Logo(version string) {
	// ASCII art logo for GOPLEXCLI - each line will be colored with a gradient
	lines := []string{
		`   ██████   ██████  ██████  ██      ███████ ██   ██  ██████ ██      ██ `,
		`  ██       ██    ██ ██   ██ ██      ██       ██ ██  ██      ██      ██ `,
		`  ██   ███ ██    ██ ██████  ██      █████     ███   ██      ██      ██ `,
		`  ██    ██ ██    ██ ██      ██      ██       ██ ██  ██      ██      ██ `,
		`   ██████   ██████  ██      ███████ ███████ ██   ██  ██████ ███████ ██ `,
	}

	// Gradient colors from light green to dark green (top to bottom)
	gradientColors := []lipgloss.Color{
		lipgloss.Color("#86EFAC"), // Light green
		lipgloss.Color("#4ADE80"), // Green 400
		lipgloss.Color("#22C55E"), // Green 500
		lipgloss.Color("#16A34A"), // Green 600
		lipgloss.Color("#15803D"), // Green 700
	}

	// Print logo with gradient
	fmt.Println()
	for i, line := range lines {
		style := lipgloss.NewStyle().
			Foreground(gradientColors[i]).
			Bold(true)
		fmt.Println(style.Render(line))
	}

	// Style for the tagline
	taglineStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")) // Gray

	// Style for the version
	versionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#4ADE80")). // Green
		Bold(true)

	fmt.Printf("\n  %s %s\n\n",
		taglineStyle.Render("Plex Media CLI ·"),
		versionStyle.Render("v"+version))
}
