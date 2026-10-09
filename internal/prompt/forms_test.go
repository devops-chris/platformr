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
