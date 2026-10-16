package template

import "testing"

func TestFuncsTakeStringLast(t *testing.T) {
	values := map[string]string{"name": "my-service", "project": "pt-payments", "teams": "DevOps,my-team", "cluster": "prod-eks"}
	cases := []struct{ tmpl, want string }{
		{`{{replace "-" "_" .name}}`, "my_service"},
		{`{{.name | replace "-" "_"}}`, "my_service"},
		{`{{replace "-" "_" (toUpper .name)}}_PORT`, "MY_SERVICE_PORT"},
		{`{{trimPrefix "pt-" .project}}`, "payments"},
		{`{{.project | trimSuffix "-payments"}}`, "pt"},
		{`{{range split "," .teams}}[{{.}}]{{end}}`, "[DevOps][my-team]"},
		{`{{if contains "prod" .cluster}}3{{else}}1{{end}}`, "3"},
		{`{{toLower "ABC"}} {{trimSpace "  x  "}}`, "abc x"},
	}
	for _, c := range cases {
		got, err := Render(c.tmpl, values)
		if err != nil {
			t.Errorf("Render(%s): %v", c.tmpl, err)
			continue
		}
		if got != c.want {
			t.Errorf("Render(%s) = %q, want %q", c.tmpl, got, c.want)
		}
	}
}

// RenderString backs computed fields, when, target_path and pr_title — the helper
// functions must work there too, not just in .tmpl files.
func TestRenderStringHasFuncs(t *testing.T) {
	got := RenderString(`{{.name | replace "-" "_"}}`, map[string]string{"name": "a-b"})
	if got != "a_b" {
		t.Errorf("RenderString = %q, want %q", got, "a_b")
	}
}

func TestMapLookupMissingKeyIsEmpty(t *testing.T) {
	maps := map[string]map[string]string{"ids": {"known": "111"}}
	cases := map[string]string{
		`{{index .maps.ids .account}}`:                        "",
		`{{if index .maps.ids .account}}yes{{else}}no{{end}}`: "no",
		`{{if not (index .maps.ids .account)}}ask{{end}}`:     "ask",
		`{{index .maps.ids "known"}}`:                         "111",
	}
	for tmpl, want := range cases {
		if got := RenderString(tmpl, map[string]string{"account": "new"}, maps); got != want {
			t.Errorf("%s = %q, want %q", tmpl, got, want)
		}
	}
}
