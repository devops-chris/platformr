package prompt

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/huh"
	"github.com/devops-chris/clihq/ui"
	"github.com/devops-chris/platformr/internal/config"
)

const ManualEntryOption = "[+ enter manually]"

// FieldContext provides runtime context for resolving dynamic field options.
type FieldContext struct {
	// ListFiles returns the names of existing resources of a given type.
	// repo and path come from the source resource's Resolved fields.
	ListFiles func(repo, path string) ([]string, error)
}

// PromptField renders an interactive prompt for a single resource field.
// Returns ManualEntryOption if the user picked "[+ enter manually]" instead of
// an existing option — the caller re-prompts as plain text in that case.
func PromptField(field config.Field, values map[string]string, ctx *FieldContext) (string, error) {
	label := field.Label
	if label == "" {
		label = field.Name
	}

	switch field.Type {
	case "select", "reviewer", "team_reviewer":
		return promptSelect(label, field, ctx)
	case "multiselect":
		return promptMultiSelect(label, field, ctx)
	default:
		return promptInput(label, field)
	}
}

// listRows is how many options a list shows before it scrolls. Without a limit a long
// list (every AWS account) fills the terminal and pushes the request heading and earlier
// answers off the top.
const listRows = 10

// searchHint is shown on long lists: huh filters a list as you type after "/".
const searchHint = "Type / to search"

func promptSelect(label string, field config.Field, ctx *FieldContext) (string, error) {
	options, err := resolveOptions(field, ctx)
	if err != nil {
		// Source unavailable — fall back to free-text input
		return promptInput(label, field)
	}

	// Nothing to pick from, and the question only matters when there is: skip it.
	if len(options) == 0 && field.SkipIfEmpty {
		return "", nil
	}

	// Nothing to pick from — go straight to a plain prompt instead of forcing a
	// select with a single "[+ enter manually]" entry and no other way out. This
	// also applies when AllowManual is set: a list of exactly one forced choice
	// isn't a real choice.
	if len(options) == 0 {
		if field.AllowManual {
			fmt.Println(ui.Subtle("Nothing found to pick from — type a value below."))
		}
		return promptInput(label, field)
	}

	if field.Optional {
		options = append([]string{"— skip —"}, options...)
	}

	if field.AllowManual {
		options = append(options, ManualEntryOption)
	}

	// Start on the default (for change requests, the current value) when it's listed.
	val := field.Default
	sel := huh.NewSelect[string]().
		Title(label).
		Options(toHuhOptions(options)...).
		Value(&val)
	long := len(options) > listRows
	if long {
		sel.Description(searchHint)
	}
	sel.WithTheme(ui.Theme())
	if long {
		sel.Height(listRows + 2) // + title and hint lines
	}
	if err := sel.Run(); err != nil {
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
	inp := huh.NewInput().
		Title(l).
		Value(&val)
	inp.WithTheme(ui.Theme())
	if err := inp.Run(); err != nil {
		return "", err
	}
	// For optional fields, treat the placeholder value as "not set" if unchanged
	if field.Optional && val == field.Placeholder {
		return "", nil
	}
	return val, nil
}

// PromptComment prompts for an optional freeform note to append to the PR body.
func PromptComment() (string, error) {
	var val string
	inp := huh.NewInput().
		Title("Additional notes for the PR? (optional, Enter to skip)").
		Value(&val)
	inp.WithTheme(ui.Theme())
	if err := inp.Run(); err != nil {
		return "", err
	}
	return val, nil
}

// resolveOptions resolves the options for a select field: "dirs:<path>" lists
// subdirectories (one instance per directory), "files:<path>" lists files minus
// their extension (one instance per file), "team:<slug>" lists team members,
// "collaborators" lists repo collaborators. For static options it returns
// field.Options directly.
func resolveOptions(field config.Field, ctx *FieldContext) ([]string, error) {
	if field.Source == "" {
		return field.Options, nil
	}
	if !strings.HasPrefix(field.Source, "dirs:") &&
		!strings.HasPrefix(field.Source, "files:") &&
		!strings.HasPrefix(field.Source, "team:") &&
		field.Source != "collaborators" {
		return nil, fmt.Errorf("unknown source %q", field.Source)
	}
	if ctx == nil || ctx.ListFiles == nil {
		return nil, fmt.Errorf("no context available to resolve source %q", field.Source)
	}
	names, err := ctx.ListFiles("", "")
	if err != nil {
		return nil, err
	}
	if field.FilterPrefix != "" {
		var filtered []string
		for _, n := range names {
			if strings.HasPrefix(n, field.FilterPrefix) {
				filtered = append(filtered, n)
			}
		}
		names = filtered
	}
	if field.FilterSuffix != "" {
		var filtered []string
		for _, n := range names {
			if strings.HasSuffix(n, field.FilterSuffix) {
				filtered = append(filtered, n)
			}
		}
		names = filtered
	}
	for i, n := range names {
		names[i] = strings.TrimSuffix(strings.TrimPrefix(n, field.StripPrefix), field.StripSuffix)
	}
	if len(field.Exclude) > 0 {
		var kept []string
		for _, n := range names {
			skip := false
			for _, x := range field.Exclude {
				skip = skip || n == x
			}
			if !skip {
				kept = append(kept, n)
			}
		}
		names = kept
	}
	return names, nil
}

// promptMultiSelect asks for any number of options from the same sources as a select.
// Options named in field.Default (comma-separated) start ticked. The answer is the
// ticked options joined with commas, in list order, e.g. "DevOps,Engineering";
// templates loop over it with split. At least one is required unless optional.
func promptMultiSelect(label string, field config.Field, ctx *FieldContext) (string, error) {
	options, err := resolveOptions(field, ctx)
	if err != nil {
		return "", err
	}
	if len(options) == 0 {
		if field.SkipIfEmpty || field.Optional {
			return "", nil
		}
		return "", fmt.Errorf("%s: there's nothing to choose from", label)
	}
	var picked []string
	long := len(options) > listRows
	hint := "Space to tick or untick, Enter when done"
	if long {
		hint += " · " + searchHint
	}
	ms := huh.NewMultiSelect[string]().
		Title(label).
		Description(hint).
		Options(multiOptions(options, field.Default)...).
		Value(&picked)
	if !field.Optional {
		ms = ms.Validate(func(v []string) error {
			if len(v) == 0 {
				return fmt.Errorf("pick at least one")
			}
			return nil
		})
	}
	ms.WithTheme(ui.Theme())
	if long {
		ms.Height(listRows + 2) // + title and hint lines
	}
	if err := ms.Run(); err != nil {
		return "", err
	}
	return joinInOrder(options, picked), nil
}

// multiOptions builds the checklist, ticking the options listed in defaults.
func multiOptions(options []string, defaults string) []huh.Option[string] {
	want := map[string]bool{}
	for _, d := range strings.Split(defaults, ",") {
		if d = strings.TrimSpace(d); d != "" {
			want[d] = true
		}
	}
	opts := make([]huh.Option[string], len(options))
	for i, o := range options {
		opts[i] = huh.NewOption(o, o).Selected(want[o])
	}
	return opts
}

// joinInOrder joins the picked options in the order they're listed, so the stored
// answer doesn't depend on the order they were ticked.
func joinInOrder(options, picked []string) string {
	got := map[string]bool{}
	for _, p := range picked {
		got[p] = true
	}
	var out []string
	for _, o := range options {
		if got[o] {
			out = append(out, o)
		}
	}
	return strings.Join(out, ",")
}

func toHuhOptions(vals []string) []huh.Option[string] {
	opts := make([]huh.Option[string], len(vals))
	for i, v := range vals {
		opts[i] = huh.NewOption(v, v)
	}
	return opts
}
