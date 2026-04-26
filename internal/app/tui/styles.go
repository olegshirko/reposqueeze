package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	primaryColor   = lipgloss.Color("#7D56F4")
	successColor   = lipgloss.Color("#04B575")
	errorColor     = lipgloss.Color("#FF4672")
	secondaryColor = lipgloss.Color("#888888")
	bgColor        = lipgloss.Color("#1A1A2E")

	// Styles
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FAFAFA")).
			Background(primaryColor).
			Padding(0, 1).
			MarginBottom(1)

	subtitleStyle = lipgloss.NewStyle().
			Foreground(secondaryColor).
			MarginBottom(1)

	focusedStyle = lipgloss.NewStyle().
			Foreground(primaryColor).
			Bold(true)

	blurredStyle = lipgloss.NewStyle().
			Foreground(secondaryColor)

	helpStyle = lipgloss.NewStyle().
			Foreground(secondaryColor).
			Italic(true)

	successStyle = lipgloss.NewStyle().
			Foreground(successColor).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(errorColor).
			Bold(true)

	logStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FAFAFA"))
)
