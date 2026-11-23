package cmd

import (
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/devops-chris/clihq/ui"
	"github.com/devops-chris/platformr/internal/config"
)

// errBack means the person picked "← Back" or pressed Esc: go to the previous menu step.
var errBack = errors.New("back")

// backValue is the "← Back" option's value. No category or request can be named this.
const backValue = "\x00back"

// menuOption is one choice in a menu step: what's shown, and the value it stands for.
type menuOption struct{ label, hint, value string }

// askFunc asks one menu question and returns the chosen value, or errBack. current is
// the choice to start on (the last one, when coming back to a step).
type askFunc func(title string, opts []menuOption, canBack bool, current string) (string, error)

// pickResource walks the menu: the thing (category), then "create or change" when that
// category has both, then the request. Each step is skipped when there's only one choice:
// a category with one request starts it straight away (its header says what started).
// From the second step on, "← Back" (or Esc) returns to the previous step asked.
// Running `request` already says you need something, so the first step has no question,
// only the list.
func pickResource(all []config.Resource, categories []config.CategoryConfig) (config.Resource, error) {
	return walkMenu(all, categories, askMenu)
}

func walkMenu(all []config.Resource, categories []config.CategoryConfig, ask askFunc) (config.Resource, error) {
	const create, change = "create", "change"
	cats := config.MenuCategories(all, categories)
	askCategory := len(cats) > 1

	category, kind := cats[0].Name, ""
	step := 0
	for {
		creates, changes := config.InMenuOrder(all, category)
		askKind := len(creates) > 0 && len(changes) > 0

		switch step {
		case 0: // the thing
			if askCategory {
				opts := make([]menuOption, len(cats))
				for i, c := range cats {
					hint := c.Description
					if hint == "" {
						cr, ch := config.InMenuOrder(all, c.Name)
						hint = fmt.Sprintf("%d request(s)", len(cr)+len(ch))
					}
					opts[i] = menuOption{c.Name, hint, c.Name}
				}
				v, err := ask("", opts, false, category)
				if err != nil {
					return config.Resource{}, err
				}
				if v != category {
					kind = ""
				}
				category = v
			}
			step = 1

		case 1: // new or change
			switch {
			case askKind:
				v, err := ask(category+": what do you want to do?", []menuOption{
					{"Create something new", "", create},
					{"Change something that already exists", "", change},
				}, askCategory, kind)
				if errors.Is(err, errBack) {
					step = 0
					continue
				}
				if err != nil {
					return config.Resource{}, err
				}
				kind = v
			case len(creates) > 0:
				kind = create
			default:
				kind = change
			}
			step = 2

		case 2: // the request
			list, title := creates, category
			if kind == change {
				list = changes
			}
			if askKind {
				title = category + ": create something new"
				if kind == change {
					title = category + ": change something that already exists"
				}
			}
			// Only one request here: start it (its header says what started) rather than
			// show a list of one.
			if len(list) == 1 {
				return list[0], nil
			}
			canBack := askKind || askCategory
			opts := make([]menuOption, len(list))
			for i, r := range list {
				opts[i] = menuOption{r.Label(), r.Description, r.Name}
			}
			v, err := ask(title, opts, canBack, "")
			if errors.Is(err, errBack) {
				if askKind {
					step = 1
				} else {
					step = 0
				}
				continue
			}
			if err != nil {
				return config.Resource{}, err
			}
			for _, r := range list {
				if r.Name == v {
					return r, nil
				}
			}
			return config.Resource{}, fmt.Errorf("resource %q not found", v)
		}
	}
}

// askMenu shows one menu step. With canBack it adds "← Back" at the bottom and Esc
// goes back too; Ctrl+C still quits.
func askMenu(title string, opts []menuOption, canBack bool, current string) (string, error) {
	if canBack {
		opts = append(opts, menuOption{"← Back", "or press Esc", backValue})
	}
	hopts := make([]huh.Option[string], len(opts))
	for i, o := range opts {
		hopts[i] = huh.NewOption(ui.PickerItem(o.label, o.hint), o.value)
	}
	v := current
	sel := huh.NewSelect[string]().Title(title).Options(hopts...).Value(&v)
	sel.WithTheme(ui.Theme())
	form := huh.NewForm(huh.NewGroup(sel)).WithShowHelp(false).WithTheme(ui.Theme())
	if !canBack {
		return v, form.Run()
	}

	form.SubmitCmd = tea.Quit
	form.CancelCmd = tea.Interrupt
	m := &escGoesBack{form: form}
	_, err := tea.NewProgram(m).Run()
	switch {
	case errors.Is(err, tea.ErrInterrupted) || form.State == huh.StateAborted:
		return "", huh.ErrUserAborted
	case err != nil:
		return "", err
	case m.back || v == backValue:
		return "", errBack
	}
	return v, nil
}

// escGoesBack runs a form, turning Esc into "go back".
type escGoesBack struct {
	form *huh.Form
	back bool
}

func (m *escGoesBack) Init() tea.Cmd { return m.form.Init() }

func (m *escGoesBack) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && k.Type == tea.KeyEsc {
		m.back = true
		return m, tea.Quit
	}
	f, cmd := m.form.Update(msg)
	if form, ok := f.(*huh.Form); ok {
		m.form = form
	}
	return m, cmd
}

func (m *escGoesBack) View() string {
	if m.back {
		return ""
	}
	return m.form.View()
}

// requestHeader is printed above a request's questions so the person knows which
// request they're in: its name, where it sits in the menu, and what it does.
func requestHeader(r config.Resource) string {
	kind := "new"
	if r.IsChange() {
		kind = "change"
	}
	if r.IsGuide() {
		kind = "guide"
	}
	h := "\n  " + ui.SectionHeader(r.Label()) + "  " + ui.Subtle(config.CategoryOf(r)+" · "+kind) + "\n"
	if r.Description != "" {
		h += "  " + ui.Subtle(r.Description) + "\n"
	}
	return h
}
