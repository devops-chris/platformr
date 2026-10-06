package edit_test

import (
	"testing"

	"github.com/devops-chris/platformr/internal/edit"
)

func TestDetectFormat(t *testing.T) {
	cases := map[string]edit.Format{
		"values.yaml": edit.YAML, "x.yml": edit.YAML, "policy.json": edit.JSON,
		"main.tf": edit.HCL, "prod.tfvars": edit.HCL, "terragrunt.hcl": edit.HCL, "job.nomad": edit.HCL,
		"main.tf.json": edit.JSON, "prod.tfvars.json": edit.JSON,
		".env": edit.Env, ".env.prod": edit.Env, "app.properties": edit.Env, "cfg.ini": edit.Env,
	}
	for file, want := range cases {
		if got, err := edit.DetectFormat(file, ""); err != nil || got != want {
			t.Errorf("DetectFormat(%s) = %s, %v; want %s", file, got, err, want)
		}
	}
	if got, err := edit.DetectFormat("job.vars", "hcl"); err != nil || got != edit.HCL {
		t.Errorf("override: %s %v", got, err)
	}
	if _, err := edit.DetectFormat("job.vars", ""); err == nil {
		t.Error("unknown extension should be an error that says to set format")
	}
	if _, err := edit.DetectFormat("x", "xml"); err == nil {
		t.Error("unknown format override should be an error")
	}
}

func TestParsePath(t *testing.T) {
	p, err := edit.ParsePath([]any{"Statement", map[string]any{"Sid": "{{.sid}}"}, "Action", int64(0)}, func(s string) string {
		if s == "{{.sid}}" {
			return "S3Read"
		}
		return s
	})
	if err != nil || p.String() != `Statement[Sid="S3Read"].Action[0]` {
		t.Errorf("got %s, %v", p, err)
	}
	if p, _ := edit.ParsePath([]any{"config", "proj:node.count"}, nil); len(p) != 2 {
		t.Errorf("list form should keep dotted keys whole: %s", p)
	}
	for _, bad := range []any{"", "a..b", []any{}, 3} {
		if _, err := edit.ParsePath(bad, nil); err == nil {
			t.Errorf("ParsePath(%v) should fail", bad)
		}
	}
}

func TestPlainErrors(t *testing.T) {
	yaml, _ := edit.Open(edit.YAML, "v.yaml", []byte("users:\n  - name: a\n  - name: a\nnote: |\n  multi\n  line\nempty:\n"))
	_, err := yaml.Apply(edit.Op{Action: "set", Path: P([]any{"users", map[string]any{"name": "a"}, "name"}), Value: "b"})
	wantErr(t, err, "matched 2 items")
	_, err = yaml.Apply(edit.Op{Action: "set", Path: P("note"), Value: "x"})
	wantErr(t, err, "multi-line text values")
	_, err = yaml.Apply(edit.Op{Action: "append", Path: P("empty"), Item: edit.Item{Value: "x"}})
	wantErr(t, err, "write it as empty: []")
	_, err = yaml.Apply(edit.Op{Action: "remove", Path: P("users"), Match: map[string]string{"name": "zed"}})
	wantErr(t, err, "nothing in users matches")
	_, err = yaml.Apply(edit.Op{Action: "set", Path: P("users"), Value: "x"})
	wantErr(t, err, "not a single value")

	js, _ := edit.Open(edit.JSON, "t.json", []byte(`{"tags": {"a": "1"}}`))
	_, err = js.Apply(edit.Op{Action: "put", Path: P("tags"), Name: "a", Item: edit.Item{Value: "2"}})
	wantErr(t, err, `already has "a"`)

	hcl, _ := edit.Open(edit.HCL, "m.tf", []byte("module \"eks\" {\n  v = \"1\"\n}\nstatement {\n  sid = \"A\"\n}\nstatement {\n  sid = \"B\"\n}\n"))
	_, err = hcl.Apply(edit.Op{Action: "set", Path: P("module.v"), Value: "2"})
	wantErr(t, err, "not found")
	_, err = hcl.Apply(edit.Op{Action: "set", Path: P("statement.sid"), Value: "C"})
	wantErr(t, err, "there are 2 statement blocks")

	env, _ := edit.Open(edit.Env, ".env", []byte("A=1\n"))
	_, err = env.Apply(edit.Op{Action: "append", Path: P("A"), Item: edit.Item{Value: "x"}})
	wantErr(t, err, "only support set")

	m, _ := edit.Open(edit.Marker, "x.ts", []byte("a = 1 // platformr:n\nb = 2 // platformr:n\n"))
	_, err = m.Apply(edit.Op{Action: "set", Path: P("marker:n"), Value: "3"})
	wantErr(t, err, "more than one line")

	// Setting a value to what it already is changes nothing.
	same, _ := edit.Open(edit.YAML, "s.yaml", []byte("a: 1\n"))
	if ch, err := same.Apply(edit.Op{Action: "set", Path: P("a"), Value: "1"}); err != nil || ch.Verb != "" {
		t.Errorf("no-op set: %v %v", ch, err)
	}
}
