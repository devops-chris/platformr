package ui

import (
	"github.com/charmbracelet/huh"
	"github.com/charmbracelet/lipgloss"
)

// Theme returns the platformr huh theme — purple palette matching the rest of the UI.
func Theme() *huh.Theme {
	t := huh.ThemeCharm()

	t.Focused.Base = t.Focused.Base.BorderForeground(colorPurple)
	t.Focused.Card = t.Focused.Base
	t.Focused.Title = lipgloss.NewStyle().Bold(true).Foreground(colorPurple)
	t.Focused.NoteTitle = lipgloss.NewStyle().Bold(true).Foreground(colorPurple).MarginBottom(1)
	t.Focused.SelectSelector = lipgloss.NewStyle().Foreground(colorPurple).SetString("▸ ")
	t.Focused.NextIndicator = lipgloss.NewStyle().MarginLeft(1).Foreground(colorPurple).SetString("↓")
	t.Focused.PrevIndicator = lipgloss.NewStyle().MarginRight(1).Foreground(colorPurple).SetString("↑")
	t.Focused.SelectedOption = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	t.Focused.FocusedButton = lipgloss.NewStyle().
		Padding(0, 2).MarginRight(1).Bold(true).
		Foreground(lipgloss.Color("#FFFDF5")).Background(colorPurple)
	t.Focused.BlurredButton = lipgloss.NewStyle().
		Padding(0, 2).MarginRight(1).
		Foreground(colorMuted).Background(lipgloss.AdaptiveColor{Light: "252", Dark: "237"})
	t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(colorGreen)
	t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(colorPurple)

	t.Blurred = t.Focused
	t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
	t.Blurred.Card = t.Blurred.Base
	t.Blurred.NextIndicator = lipgloss.NewStyle()
	t.Blurred.PrevIndicator = lipgloss.NewStyle()

	t.Group.Title = t.Focused.Title
	t.Group.Description = t.Focused.Description
	return t
}
