package cmd

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
	"github.com/devops-chris/platformr/internal/config"
)

// scripted answers menu questions in order and records what was asked.
type scripted struct {
	answers []string // a value, or "BACK"
	asked   []string // "title|canBack|current"
}

func (s *scripted) ask(title string, opts []menuOption, canBack bool, current string) (string, error) {
	back := "noback"
	if canBack {
		back = "back"
	}
	s.asked = append(s.asked, title+"|"+back+"|"+current)
	a := s.answers[0]
	s.answers = s.answers[1:]
	if a == "BACK" {
		return "", errBack
	}
	return a, nil
}

var menuResources = []config.Resource{
	{Name: "vpc", Category: "Network"},
	{Name: "twingate", Category: "Network"},
	{Name: "eks", Category: "Kubernetes"},
	{Name: "eks-upgrade", Category: "Kubernetes", Action: "change"},
	{Name: "rds", Category: "Database"},
}

func TestWalkMenu(t *testing.T) {
	cases := []struct {
		name    string
		answers []string
		want    string
		asked   string
	}{
		{"straight through", []string{"Kubernetes", "change", "eks-upgrade"}, "eks-upgrade",
			"What do you need?|noback|Network|Kubernetes: what do you want to do?|back||Kubernetes: change an existing one|back|"},
		{"a single request is still shown, with Back", []string{"Database", "rds"}, "rds",
			"What do you need?|noback|Network|Database: create a new one|back|"},
		{"back from a single-request list", []string{"Database", "BACK", "Kubernetes", "change", "eks-upgrade"}, "eks-upgrade",
			"What do you need?|noback|Network|Database: create a new one|back||What do you need?|noback|Database|Kubernetes: what do you want to do?|back||Kubernetes: change an existing one|back|"},
		{"back from new/change to the thing, starting on the last pick",
			[]string{"Kubernetes", "BACK", "Network", "twingate"}, "twingate",
			"What do you need?|noback|Network|Kubernetes: what do you want to do?|back||What do you need?|noback|Kubernetes|Network: create a new one|back|"},
		{"back from the list skips the unasked new/change step",
			[]string{"Network", "BACK", "Database", "rds"}, "rds",
			"What do you need?|noback|Network|Network: create a new one|back||What do you need?|noback|Network|Database: create a new one|back|"},
		{"back from the list to new/change, starting on the last pick",
			[]string{"Kubernetes", "create", "BACK", "change", "eks-upgrade"}, "eks-upgrade",
			"What do you need?|noback|Network|Kubernetes: what do you want to do?|back||Kubernetes: create a new one|back||Kubernetes: what do you want to do?|back|create|Kubernetes: change an existing one|back|"},
	}
	// "create" in Kubernetes has one request, so it's picked without a list; use a second one
	// for the back-from-list case.
	res := append(append([]config.Resource{}, menuResources...), config.Resource{Name: "eks-addon", Category: "Kubernetes"})
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &scripted{answers: c.answers}
			if c.name == "back from the list to new/change, starting on the last pick" {
				got, err := walkMenu(res, nil, s.ask)
				if err != nil || got.Name != c.want {
					t.Fatalf("got %q, %v; want %q", got.Name, err, c.want)
				}
			} else {
				got, err := walkMenu(menuResources, nil, s.ask)
				if err != nil || got.Name != c.want {
					t.Fatalf("got %q, %v; want %q", got.Name, err, c.want)
				}
			}
			if strings.Join(s.asked, "|") != c.asked {
				t.Errorf("asked:\n  %s\nwant:\n  %s", strings.Join(s.asked, "|"), c.asked)
			}
		})
	}
}

func TestWalkMenuOneCategoryHasNoBackOnFirstAskedStep(t *testing.T) {
	only := []config.Resource{{Name: "a"}, {Name: "b"}}
	s := &scripted{answers: []string{"b"}}
	got, err := walkMenu(only, nil, s.ask)
	if err != nil || got.Name != "b" {
		t.Fatalf("got %q, %v", got.Name, err)
	}
	if len(s.asked) != 1 || !strings.Contains(s.asked[0], "|noback|") {
		t.Errorf("the only step asked shouldn't offer Back: %v", s.asked)
	}
}

func TestWalkMenuQuitStops(t *testing.T) {
	quit := errors.New("quit")
	_, err := walkMenu(menuResources, nil, func(string, []menuOption, bool, string) (string, error) { return "", quit })
	if !errors.Is(err, quit) {
		t.Errorf("err = %v, want quit passed through", err)
	}
}

func TestEscGoesBack(t *testing.T) {
	v := ""
	form := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Options(huh.NewOptions("a", "b")...).Value(&v)))
	m := &escGoesBack{form: form}
	m.Init()
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if !m.back {
		t.Fatal("Esc didn't mark back")
	}
	if cmd == nil {
		t.Fatal("Esc should quit the step")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Error("Esc should send tea.Quit")
	}

	// Other keys go to the form as usual.
	m2 := &escGoesBack{form: huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Options(huh.NewOptions("a", "b")...).Value(&v)))}
	m2.Init()
	m2.Update(tea.KeyMsg{Type: tea.KeyDown})
	if m2.back {
		t.Error("Down shouldn't mean back")
	}
}

func TestRequestHeader(t *testing.T) {
	h := requestHeader(config.Resource{Name: "rds", DisplayName: "New Aurora PostgreSQL database", Category: "Database", Description: "Create an Aurora database"})
	for _, want := range []string{"New Aurora PostgreSQL database", "Database · new", "Create an Aurora database"} {
		if !strings.Contains(h, want) {
			t.Errorf("header missing %q:\n%s", want, h)
		}
	}
	h = requestHeader(config.Resource{Name: "eks-upgrade", Action: "change"})
	if !strings.Contains(h, "eks-upgrade") || !strings.Contains(h, "General · change") {
		t.Errorf("change request header:\n%s", h)
	}
}
