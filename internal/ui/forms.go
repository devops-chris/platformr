package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/devops-chris/platformr/internal/config"
)

const CreateNewOption = "[+ create new]"

// FieldContext provides runtime context for resolving dynamic field options.
type FieldContext struct {
	// ListFiles returns the names of existing resources of a given type.
	// repo and path come from the source resource's Resolved fields.
	ListFiles func(repo, path string) ([]string, error)
}

// PromptField renders an interactive prompt for a single resource field.
// Returns CreateNewOption if the user chose to create a new dependency resource.
func PromptField(field config.Field, values map[string]string, ctx *FieldContext) (string, error) {
	label := field.Label
	if label == "" {
		label = field.Name
	}

	switch field.Type {
	case "select":
		return promptSelect(label, field, ctx)
	default:
		return promptInput(label, field)
	}
}

func promptSelect(label string, field config.Field, ctx *FieldContext) (string, error) {
	options, err := resolveOptions(field, ctx)
	if err != nil {
		// Source unavailable — fall back to free-text input
		return promptInput(label, field)
	}

	if field.Optional {
		options = append([]string{"— skip —"}, options...)
	}

	if field.AllowCreate {
		options = append(options, CreateNewOption)
	}

	// No options and no create — fall back to input
	if len(options) == 0 {
		return promptInput(label, field)
	}

	var val string
	if err := huh.NewSelect[string]().
		Title(label).
		Options(toHuhOptions(options)...).
		Value(&val).
		Run(); err != nil {
		return "", err
	}
	if val == "— skip —" {
		return "", nil
	}
	return val, nil
}

func promptInput(label string, field config.Field) (string, error) {
	val := field.Default
	if val == "" {
		val = field.Placeholder
	}
	l := label
	if field.Optional {
		l = label + " (optional)"
	}
	if err := huh.NewInput().
		Title(l).
		Value(&val).
		Run(); err != nil {
		return "", err
	}
	// For optional fields, treat the placeholder value as "not set" if unchanged
	if field.Optional && val == field.Placeholder {
		return "", nil
	}
	return val, nil
}

// resolveOptions resolves the options for a select field.
// For "resource.<type>" sources it calls ctx.ListFiles with the resolved coordinates.
// For static options it returns field.Options directly.
func resolveOptions(field config.Field, ctx *FieldContext) ([]string, error) {
	if field.Source == "" {
		return field.Options, nil
	}
	if !strings.HasPrefix(field.Source, "resource.") &&
		!strings.HasPrefix(field.Source, "dirs:") &&
		!strings.HasPrefix(field.Source, "team:") &&
		field.Source != "collaborators" {
		return nil, fmt.Errorf("unknown source %q", field.Source)
	}
	if ctx == nil || ctx.ListFiles == nil {
		return nil, fmt.Errorf("no context available to resolve source %q", field.Source)
	}
	// Source resolution (repo + path) is handled by the caller via ctx.ListFiles.
	// This is intentionally a thin wrapper — the caller sets up the closure.
	return ctx.ListFiles("", "")
}

// PromptComment prompts for an optional freeform note to append to the PR body.
func PromptComment() (string, error) {
	var val string
	if err := huh.NewInput().
		Title("Additional notes for the PR? (optional, Enter to skip)").
		Value(&val).
		Run(); err != nil {
		return "", err
	}
	return val, nil
}

func toHuhOptions(vals []string) []huh.Option[string] {
	opts := make([]huh.Option[string], len(vals))
	for i, v := range vals {
		opts[i] = huh.NewOption(v, v)
	}
	return opts
}
