package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/devops-chris/platformr/internal/config"
	"github.com/devops-chris/platformr/internal/edit"
)

// TestChangeableExamples runs each example in examples/changeable through the real
// change-request code: its own platformr.toml, its own repo files, the answers a
// developer would give. The result must equal the example's expected/ file exactly.
func TestChangeableExamples(t *testing.T) {
	cases := []struct {
		dir, request, expected string
		answers                map[string]string
		wantSummary            string
	}{
		{"helm-scale-service", "service-scale", "expected",
			map[string]string{"name": "payments", "replicas": "4", "cpu": "1"},
			"How many copies?: 2 → 4 | CPU per copy: 500m → 1"},
		{"terraform-eks-version", "eks-upgrade", "expected",
			map[string]string{"name": "apps", "cluster_version": "1.33"},
			"Kubernetes version (one step at a time, e.g. 1.32 → 1.33): 1.32 → 1.33"},
		{"security-group-cidr", "sg-allow-ip", "expected-allow",
			map[string]string{"cidr": "203.0.113.10/32"},
			"resource.aws_security_group_rule.https.cidr_blocks: + 203.0.113.10/32"},
		{"security-group-cidr", "sg-remove-ip", "expected-remove",
			map[string]string{"cidr": "198.51.100.0/24"},
			"resource.aws_security_group_rule.https.cidr_blocks: − 198.51.100.0/24"},
		{"iam-policy-action", "iam-add-action", "expected",
			map[string]string{"policy": "reports", "sid": "ReadReports", "action": "s3:PutObject"},
			`Statement[Sid="ReadReports"].Action: + s3:PutObject`},
	}
	for _, c := range cases {
		t.Run(c.request, func(t *testing.T) {
			root := filepath.Join("..", "examples", "changeable", c.dir)
			var rc config.RepoConfig
			if _, err := toml.DecodeFile(filepath.Join(root, "platformr.toml"), &rc); err != nil {
				t.Fatal(err)
			}
			rc.RepoName = "acme/infra"
			config.Resolve(&config.OrgConfig{}, &rc)
			var r config.Resource
			for _, res := range rc.Resources {
				if res.Name == c.request {
					r = res
				}
			}
			if errs := checkUpdateConfig(r); len(errs) > 0 {
				t.Fatalf("config problems: %v", errs)
			}
			sess := &updateSession{
				repo: "acme/infra", branch: "main",
				fetch: func(p string) (string, error) {
					b, err := os.ReadFile(filepath.Join(root, p))
					if os.IsNotExist(err) {
						return "", errFileNotFound
					}
					return string(b), err
				},
				exists: func(p string) (bool, error) { _, err := os.Stat(filepath.Join(root, p)); return err == nil, nil },
				docs:   map[string]edit.Doc{}, original: map[string][]byte{},
			}
			// Questions with key/list are prepared from the file, as in a real run.
			for _, f := range r.Fields {
				if f.Key != nil || f.List != nil {
					if _, err := sess.prepareField(f, r, c.answers); err != nil {
						t.Fatalf("question %s: %v", f.Name, err)
					}
				}
			}
			changes, files, err := sess.apply(r, c.answers)
			if err != nil {
				t.Fatal(err)
			}
			var summary string
			for i, ch := range changes {
				if i > 0 {
					summary += " | "
				}
				summary += ch.String()
			}
			if summary != c.wantSummary {
				t.Errorf("summary\n got %s\nwant %s", summary, c.wantSummary)
			}
			if len(files) != 1 {
				t.Fatalf("want 1 changed file, got %d", len(files))
			}
			want, err := os.ReadFile(filepath.Join(root, c.expected, files[0].Path))
			if err != nil {
				t.Fatal(err)
			}
			if files[0].Content != string(want) {
				t.Errorf("%s\n--- got ---\n%s\n--- want ---\n%s", files[0].Path, files[0].Content, want)
			}
		})
	}
}
