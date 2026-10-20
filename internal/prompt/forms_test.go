package prompt

import (
	"strings"
	"testing"

	"github.com/devops-chris/platformr/internal/config"
)

func TestResolveOptionsSuffixAndExclude(t *testing.T) {
	files := []string{"chiro-ctc-accounts", "ortho-tops-accounts", "pt-corp-accounts", "pt-sandbox", "labels", "locals"}
	field := config.Field{
		Source:       "files:cloud/aws/pt-management/global/organization",
		FilterSuffix: "-accounts",
		StripSuffix:  "-accounts",
		Exclude:      []string{"pt-corp"},
	}
	got, err := resolveOptions(field, &FieldContext{ListFiles: func(_, _ string) ([]string, error) {
		return append([]string(nil), files...), nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, ",") != "chiro-ctc,ortho-tops" {
		t.Errorf("got %v", got)
	}
}

func TestSkipIfEmpty(t *testing.T) {
	field := config.Field{Name: "env_folder", Type: "select", Source: "dirs:x", SkipIfEmpty: true}
	ctx := &FieldContext{ListFiles: func(_, _ string) ([]string, error) { return nil, nil }}
	got, err := promptSelect("Environment", field, ctx)
	if err != nil || got != "" {
		t.Errorf("empty list with skip_if_empty should skip without prompting: %q %v", got, err)
	}
}

func TestMultiSelectHelpers(t *testing.T) {
	opts := multiOptions([]string{"DevOps", "Engineering", "pt-ortho-tops-developer"}, "Engineering, DevOps")
	var ticked []string
	for _, o := range opts {
		if o.Value == "DevOps" || o.Value == "Engineering" {
			ticked = append(ticked, o.Value)
		}
	}
	if len(ticked) != 2 {
		t.Errorf("defaults not found in options: %v", ticked)
	}
	got := joinInOrder([]string{"DevOps", "Engineering", "pt-ortho-tops-developer"}, []string{"pt-ortho-tops-developer", "DevOps"})
	if got != "DevOps,pt-ortho-tops-developer" {
		t.Errorf("joinInOrder = %q", got)
	}
}

func TestMultiSelectSkipIfEmpty(t *testing.T) {
	field := config.Field{Name: "groups", Type: "multiselect", Source: "dirs:x", SkipIfEmpty: true}
	ctx := &FieldContext{ListFiles: func(_, _ string) ([]string, error) { return nil, nil }}
	if got, err := promptMultiSelect("Groups", field, ctx); err != nil || got != "" {
		t.Errorf("empty + skip_if_empty: %q %v", got, err)
	}
	field.SkipIfEmpty = false
	if _, err := promptMultiSelect("Groups", field, ctx); err == nil {
		t.Error("empty and required should be an error")
	}
}

func TestSkipText(t *testing.T) {
	if got := SkipText(config.Field{Optional: true}); got != "— skip —" {
		t.Errorf("default = %q", got)
	}
	if got := SkipText(config.Field{Optional: true, SkipLabel: "prod (this account's own, no folder)"}); got != "prod (this account's own, no folder)" {
		t.Errorf("skip_label = %q", got)
	}
}
