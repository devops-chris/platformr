// Package changeable holds the two examples from docs/changing-existing-resources.md.
// This test is the planned "edited by hand?" check in miniature: render each
// platformr-owned template with the answers the resource was created with, and
// compare the result to the file in the repo. Same text = nobody edited it by hand,
// so a change request can safely rewrite it.
package changeable

import (
	"os"
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/devops-chris/platformr/internal/config"
	"github.com/devops-chris/platformr/internal/template"
)

func TestOwnedFilesMatchTheirTemplates(t *testing.T) {
	cases := []struct {
		name, template, owned string
		answers               map[string]string
	}{
		{
			name:     "helm values-platformr.yaml",
			template: "helm-scale-service/platformr/templates/service/values-platformr.yaml.tmpl",
			owned:    "helm-scale-service/apps/payments/values-platformr.yaml",
			answers:  map[string]string{"replicas": "2", "cpu": "500m"},
		},
		{
			name:     "terraform instance.auto.tfvars",
			template: "terraform-eks-version/platformr/templates/eks/instance.auto.tfvars.tmpl",
			owned:    "terraform-eks-version/clusters/apps/instance.auto.tfvars",
			answers:  map[string]string{"cluster_version": "1.32", "node_count": "3"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpl, err := os.ReadFile(c.template)
			if err != nil {
				t.Fatal(err)
			}
			want, err := os.ReadFile(c.owned)
			if err != nil {
				t.Fatal(err)
			}
			got, err := template.Render(string(tmpl), c.answers)
			if err != nil {
				t.Fatal(err)
			}
			if got != string(want) {
				t.Errorf("%s doesn't match its template — edited by hand?\n--- rendered ---\n%s\n--- in repo ---\n%s", c.owned, got, want)
			}
		})
	}
}

// The example platformr.toml files must load with today's config types.
func TestExampleConfigsParse(t *testing.T) {
	for _, f := range []string{"helm-scale-service/platformr.toml", "terraform-eks-version/platformr.toml"} {
		var rc config.RepoConfig
		if _, err := toml.DecodeFile(f, &rc); err != nil {
			t.Errorf("%s: %v", f, err)
		}
		if len(rc.Resources) != 2 {
			t.Errorf("%s: want 2 requests, got %d", f, len(rc.Resources))
		}
	}
}

// A change request = same template, recorded answers, plus the one new answer.
// The result must differ from today's file on exactly one line: the one asked about.
func TestChangeRequestChangesOnlyWhatWasAsked(t *testing.T) {
	cases := []struct {
		name, template, owned, wantLine string
		answers                         map[string]string
	}{
		{
			name:     "scale service: replicas 2 → 4",
			template: "helm-scale-service/platformr/templates/service/values-platformr.yaml.tmpl",
			owned:    "helm-scale-service/apps/payments/values-platformr.yaml",
			answers:  map[string]string{"replicas": "4", "cpu": "500m"},
			wantLine: "replicaCount: 4",
		},
		{
			name:     "upgrade cluster: 1.32 → 1.33",
			template: "terraform-eks-version/platformr/templates/eks/instance.auto.tfvars.tmpl",
			owned:    "terraform-eks-version/clusters/apps/instance.auto.tfvars",
			answers:  map[string]string{"cluster_version": "1.33", "node_count": "3"},
			wantLine: `cluster_version = "1.33"`,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tmpl, _ := os.ReadFile(c.template)
			before, _ := os.ReadFile(c.owned)
			after, err := template.Render(string(tmpl), c.answers)
			if err != nil {
				t.Fatal(err)
			}
			b, a := strings.Split(string(before), "\n"), strings.Split(after, "\n")
			if len(a) != len(b) {
				t.Fatalf("line count changed: %d → %d", len(b), len(a))
			}
			var changed []string
			for i := range a {
				if a[i] != b[i] {
					changed = append(changed, a[i])
				}
			}
			if len(changed) != 1 || changed[0] != c.wantLine {
				t.Errorf("changed lines = %q, want only %q", changed, c.wantLine)
			}
		})
	}
}
