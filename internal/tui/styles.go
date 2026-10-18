package tui

import "charm.land/lipgloss/v2"

var (
	colAccent  = lipgloss.Color("#5FAFD7")
	colMuted   = lipgloss.Color("#808080")
	colGood    = lipgloss.Color("#87D787")
	colWarn    = lipgloss.Color("#FFD75F")
	colBad     = lipgloss.Color("#FF5F5F")
	colPending = lipgloss.Color("#D787FF")
	colBorder  = lipgloss.Color("#4E4E4E")

	stTitle    = lipgloss.NewStyle().Bold(true).Foreground(colAccent)
	stMuted    = lipgloss.NewStyle().Foreground(colMuted)
	stBold     = lipgloss.NewStyle().Bold(true)
	stGood     = lipgloss.NewStyle().Foreground(colGood)
	stWarn     = lipgloss.NewStyle().Foreground(colWarn)
	stBad      = lipgloss.NewStyle().Foreground(colBad).Bold(true)
	stPending  = lipgloss.NewStyle().Foreground(colPending).Bold(true)
	stSection  = lipgloss.NewStyle().Foreground(colAccent).Bold(true).MarginTop(1)
	stSelected = lipgloss.NewStyle().Reverse(true)
	stKey      = lipgloss.NewStyle().Foreground(colAccent).Bold(true)

	stTab       = lipgloss.NewStyle().Padding(0, 1).Foreground(colMuted)
	stTabActive = lipgloss.NewStyle().Padding(0, 1).Bold(true).Foreground(lipgloss.Color("#000000")).Background(colAccent)

	stPanel = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colBorder).Padding(0, 1)
	stModal = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(colAccent).Padding(1, 2)
	stBig   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#000000")).Background(colAccent).Padding(0, 2)
)
