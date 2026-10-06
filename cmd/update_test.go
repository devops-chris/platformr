package cmd

import (
	"strings"
	"testing"

	"github.com/BurntSushi/toml"
	"github.com/devops-chris/platformr/internal/config"
	"github.com/devops-chris/platformr/internal/edit"
)

// Files in the pretend target repo.
var repoFiles = map[string]string{
	"clusters/apps/instance.hcl": `locals {
  cluster_version = "1.32"
  node_count      = 3
}
`,
	"clusters/apps/access.yaml": `users:
  - name: jane
    role: admin
`,
	"network/sg.auto.tfvars": `allowed_cidrs = [
  "10.0.0.0/8",
]
`,
	"clusters/apps/users/raj.yaml": "name: raj\n",
}

const requestsTOML = `
[[resources]]
name        = "eks-upgrade"
update_file = "clusters/{{.name}}/instance.hcl"

  [[resources.fields]]
  name    = "name"
  type    = "select"
  options = ["apps"]

  [[resources.fields]]
  name    = "cluster_version"
  type    = "select"
  label   = "Kubernetes version"
  options = ["1.33", "1.34"]
  key     = "locals.cluster_version"

[[resources]]
name        = "eks-add-user"
update_file = "clusters/{{.name}}/access.yaml"

  [[resources.fields]]
  name = "username"
  type = "input"

  [[resources.fields]]
  name = "role"
  type = "input"

  [[resources.changes]]
  action    = "append"
  key       = "users"
  item      = { name = "{{.username}}", role = "{{.role}}" }
  unique_by = "name"

[[resources]]
name        = "eks-remove-user"
update_file = "clusters/{{.name}}/access.yaml"

  [[resources.fields]]
  name = "username"
  type = "select"
  list = "users"
  show = "name"

  [[resources.changes]]
  action = "remove"
  key    = "users"
  match  = { name = "{{.username}}" }

[[resources]]
name        = "sg-allow-ip"
update_file = "network/sg.auto.tfvars"

  [[resources.fields]]
  name = "cidr"
  type = "input"

  [[resources.changes]]
  action    = "append"
  key       = "allowed_cidrs"
  item      = "{{.cidr}}"
  unique_by = "value"

[[resources]]
name        = "add-ou"
update_file = "clusters/{{.name}}/access.yaml"

  [[resources.changes]]
  action    = "append"
  key       = "users"
  item      = { name = "{{.username}}", role = "admin" }
  unique_by = "name"
  if_exists = "skip"

[[resources]]
name = "eks-remove-user-file"

  [[resources.changes]]
  action = "delete_file"
  file   = "clusters/{{.name}}/users/{{.username}}.yaml"
`

func testRequest(t *testing.T, name string) (config.Resource, *updateSession) {
	t.Helper()
	var rc config.RepoConfig
	if _, err := toml.Decode(requestsTOML, &rc); err != nil {
		t.Fatal(err)
	}
	rc.RepoName = "acme/infra"
	config.Resolve(&config.OrgConfig{}, &rc)
	for _, r := range rc.Resources {
		if r.Name == name {
			if !r.IsUpdate() {
				t.Fatalf("%s should be a change request", name)
			}
			sess := &updateSession{
				repo: "acme/infra", branch: "main",
				fetch: func(p string) (string, error) {
					if c, ok := repoFiles[p]; ok {
						return c, nil
					}
					return "", errFileNotFound
				},
				exists: func(p string) (bool, error) { _, ok := repoFiles[p]; return ok, nil },
				docs:   map[string]edit.Doc{}, original: map[string][]byte{},
			}
			return r, sess
		}
	}
	t.Fatalf("no request %s", name)
	return config.Resource{}, nil
}

func TestUpgradeStartsOnCurrentAndChangesOneLine(t *testing.T) {
	r, sess := testRequest(t, "eks-upgrade")
	values := map[string]string{"name": "apps"}
	f, err := sess.prepareField(r.Fields[1], r, values)
	if err != nil {
		t.Fatal(err)
	}
	if f.Default != "1.32" || f.Label != "Kubernetes version (now: 1.32)" {
		t.Errorf("default %q label %q", f.Default, f.Label)
	}
	if strings.Join(f.Options, ",") != "1.32,1.33,1.34" {
		t.Errorf("current value should stay pickable: %v", f.Options)
	}

	values["cluster_version"] = "1.33"
	changes, files, err := sess.apply(r, values)
	if err != nil {
		t.Fatal(err)
	}
	if len(changes) != 1 || changes[0].String() != "Kubernetes version: 1.32 → 1.33" {
		t.Errorf("changes = %v", changes)
	}
	if len(files) != 1 || !strings.Contains(files[0].Content, `cluster_version = "1.33"`) || !strings.Contains(files[0].Content, "node_count      = 3") {
		t.Errorf("files = %+v", files)
	}

	// Picking the value that's already there changes nothing.
	r, sess = testRequest(t, "eks-upgrade")
	_, files, err = sess.apply(r, map[string]string{"name": "apps", "cluster_version": "1.32"})
	if err != nil || len(files) != 0 {
		t.Errorf("no-op should produce no files: %v %v", files, err)
	}
}

func TestAddAndRemoveUser(t *testing.T) {
	r, sess := testRequest(t, "eks-add-user")
	changes, files, err := sess.apply(r, map[string]string{"name": "apps", "username": "lee", "role": "read-only"})
	if err != nil {
		t.Fatal(err)
	}
	want := "users:\n  - name: jane\n    role: admin\n  - name: lee\n    role: read-only\n"
	if len(files) != 1 || files[0].Content != want || changes[0].String() != "users: + {name=lee, role=read-only}" {
		t.Errorf("got %q / %v", files[0].Content, changes)
	}

	r, sess = testRequest(t, "eks-add-user")
	_, _, err = sess.apply(r, map[string]string{"name": "apps", "username": "jane", "role": "admin"})
	if err == nil || !strings.Contains(err.Error(), "jane is already in users") {
		t.Errorf("duplicate add: %v", err)
	}

	r, sess = testRequest(t, "eks-remove-user")
	values := map[string]string{"name": "apps"}
	f, err := sess.prepareField(r.Fields[0], r, values)
	if err != nil || strings.Join(f.Options, ",") != "jane" {
		t.Fatalf("picker options %v, %v", f.Options, err)
	}
	values["username"] = "jane"
	_, files, err = sess.apply(r, values)
	if err != nil || files[0].Content != "users: []\n" {
		t.Errorf("remove: %q %v", files[0].Content, err)
	}
}

func TestAllowIPOnSecurityGroup(t *testing.T) {
	r, sess := testRequest(t, "sg-allow-ip")
	_, files, err := sess.apply(r, map[string]string{"cidr": "203.0.113.10/32"})
	if err != nil {
		t.Fatal(err)
	}
	want := "allowed_cidrs = [\n  \"10.0.0.0/8\",\n  \"203.0.113.10/32\",\n]\n"
	if files[0].Content != want {
		t.Errorf("got %q", files[0].Content)
	}
	r, sess = testRequest(t, "sg-allow-ip")
	if _, _, err := sess.apply(r, map[string]string{"cidr": "10.0.0.0/8"}); err == nil {
		t.Error("adding a CIDR that's already there should stop")
	}
}

func TestDeleteFileAndMissingFile(t *testing.T) {
	r, sess := testRequest(t, "eks-remove-user-file")
	changes, files, err := sess.apply(r, map[string]string{"name": "apps", "username": "raj"})
	if err != nil || len(files) != 1 || !files[0].Delete || files[0].Path != "clusters/apps/users/raj.yaml" {
		t.Fatalf("files %+v err %v", files, err)
	}
	if changes[0].String() != "file: − clusters/apps/users/raj.yaml" {
		t.Errorf("summary %v", changes)
	}
	r, sess = testRequest(t, "eks-remove-user-file")
	if _, _, err := sess.apply(r, map[string]string{"name": "apps", "username": "zed"}); err == nil || !strings.Contains(err.Error(), "doesn't exist") {
		t.Errorf("missing file: %v", err)
	}
	r, sess = testRequest(t, "eks-upgrade")
	if _, err := sess.prepareField(r.Fields[1], r, map[string]string{"name": "nope"}); err == nil || !strings.Contains(err.Error(), "clusters/nope/instance.hcl doesn't exist") {
		t.Errorf("missing update_file: %v", err)
	}
}

func TestLineDiff(t *testing.T) {
	got := strings.Join(lineDiff("a\nb\nc\nd\ne\n", "a\nb\nC\nd\ne\n"), "\n")
	want := "  a\n  b\n- c\n+ C\n  d\n  e"
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

func TestCheckUpdateConfig(t *testing.T) {
	var rc config.RepoConfig
	_, err := toml.Decode(`
[[resources]]
name = "bad"
update_file = "x/job.vars"
  [[resources.fields]]
  name = "a"
  key  = "a"
  [[resources.changes]]
  action = "put"
  key    = "m"
  item   = "v"
  [[resources.changes]]
  action = "rename"
`, &rc)
	if err != nil {
		t.Fatal(err)
	}
	var msgs []string
	for _, e := range checkUpdateConfig(rc.Resources[0]) {
		msgs = append(msgs, e.Error())
	}
	all := strings.Join(msgs, "\n")
	for _, want := range []string{"doesn't know what format x/job.vars is", "change #1 (put) needs name", "change #2 (rename): unknown action"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in:\n%s", want, all)
		}
	}
	for _, name := range []string{"eks-upgrade", "eks-add-user", "eks-remove-user", "sg-allow-ip", "eks-remove-user-file"} {
		r, _ := testRequest(t, name)
		if errs := checkUpdateConfig(r); len(errs) > 0 {
			t.Errorf("%s: %v", name, errs)
		}
	}
}

func TestAppendIfExistsSkip(t *testing.T) {
	r, sess := testRequest(t, "add-ou")
	changes, files, err := sess.apply(r, map[string]string{"name": "apps", "username": "jane"})
	if err != nil || len(files) != 0 || len(changes) != 0 {
		t.Errorf("already there + skip should change nothing: %v %v %v", changes, files, err)
	}
	r, sess = testRequest(t, "add-ou")
	_, files, err = sess.apply(r, map[string]string{"name": "apps", "username": "lee"})
	if err != nil || len(files) != 1 {
		t.Errorf("new item should still be added: %v %v", files, err)
	}
}
