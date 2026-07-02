package ui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

var (
	colorPurple = lipgloss.AdaptiveColor{Light: "#874BFD", Dark: "#7D56F4"}
	colorGreen  = lipgloss.AdaptiveColor{Light: "#43BF6D", Dark: "#73F59F"}
	colorRed    = lipgloss.AdaptiveColor{Light: "#D0342C", Dark: "#FF4672"}
	colorYellow = lipgloss.AdaptiveColor{Light: "#A07A10", Dark: "#FFCC66"}
	colorMuted  = lipgloss.AdaptiveColor{Light: "#9B9B9B", Dark: "#5C5C5C"}

	successStyle = lipgloss.NewStyle().Foreground(colorGreen).Bold(true)
	errorStyle   = lipgloss.NewStyle().Foreground(colorRed).Bold(true)
	warningStyle = lipgloss.NewStyle().Foreground(colorYellow).Bold(true)
	subtleStyle  = lipgloss.NewStyle().Foreground(colorMuted)

	bannerStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorPurple).
			Padding(1, 3).
			Width(52)

	bannerTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(colorPurple)

	bannerSubtitleStyle = lipgloss.NewStyle().
				Faint(true)
)

func Banner() string {
	content := lipgloss.JoinVertical(lipgloss.Left,
		bannerTitleStyle.Render("platformr"),
		bannerSubtitleStyle.Render("developer self-service platform CLI"),
	)
	return bannerStyle.Render(content)
}

var (
	pickerCategoryStyle = lipgloss.NewStyle().Bold(true).Foreground(colorPurple)
	pickerNameStyle     = lipgloss.NewStyle().Bold(true)
)

func Success(msg string) string   { return successStyle.Render("  " + msg) }
func Error(msg string) string     { return errorStyle.Render("  " + msg) }
func Warning(msg string) string   { return warningStyle.Render("  " + msg) }
func Subtle(msg string) string    { return subtleStyle.Render(msg) }
func Highlight(msg string) string { return bannerTitleStyle.Render(msg) }

// PickerCategory renders a category header for static output (e.g. catalog list).
func PickerCategory(cat string) string {
	return pickerCategoryStyle.Render("  ◆ " + cat)
}

// CategoryOption renders a category name + resource count for use as a selectable huh option.
func CategoryOption(cat string, count int) string {
	noun := "resources"
	if count == 1 {
		noun = "resource"
	}
	return fmt.Sprintf("%s  %s", pickerCategoryStyle.Render(cat), subtleStyle.Render(fmt.Sprintf("%d %s", count, noun)))
}

// PickerItem renders a resource name + description for use as a huh select option label.
func PickerItem(name, desc string) string {
	return fmt.Sprintf("%s  %s", pickerNameStyle.Render(fmt.Sprintf("%-18s", name)), subtleStyle.Render(desc))
}
