package cmd

import (
	"strings"
	"testing"
)

func TestLexerFor(t *testing.T) {
	for file, want := range map[string]string{
		"main.tf": "Terraform", "terragrunt.hcl": "HCL", "prod.tfvars": "Terraform",
		"values.yaml": "YAML", "policy.json": "JSON", "platformr.toml": "TOML", ".env": "Bash",
	} {
		l := lexerFor(file)
		if l == nil || l.Config().Name != want {
			t.Errorf("%s: got %v, want %s", file, l, want)
		}
	}
	if lexerFor("README") != nil {
		t.Error("unknown types stay plain")
	}
}

func TestHighlightRespectsNoColor(t *testing.T) {
	t.Setenv("FORCE_COLOR", "1")
	if !strings.Contains(highlight("main.tf", "a = 1\n"), "\x1b[") {
		t.Error("FORCE_COLOR should produce colors")
	}
	t.Setenv("NO_COLOR", "1")
	if got := highlight("main.tf", "a = 1\n"); got != "a = 1\n" {
		t.Errorf("NO_COLOR should leave text alone, got %q", got)
	}
}

func TestThemeName(t *testing.T) {
	t.Setenv("PLATFORMR_THEME", "Nord")
	if themeName() != "nord" {
		t.Errorf("override: %s", themeName())
	}
	t.Setenv("PLATFORMR_THEME", "not-a-theme")
	if n := themeName(); n != "github" && n != "github-dark" {
		t.Errorf("unknown theme should fall back to GitHub, got %s", n)
	}
}
