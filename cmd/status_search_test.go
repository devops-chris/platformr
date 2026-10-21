package cmd

import "testing"

func TestSearchQuery(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{nil, ""},
		{[]string{"orders"}, "orders"},
		{[]string{"billing", "db"}, "billing db"},
		{[]string{"billing db"}, `"billing db"`},
		{[]string{`"already quoted"`}, `"already quoted"`},
	}
	for _, c := range cases {
		if got := searchQuery(c.args); got != c.want {
			t.Errorf("searchQuery(%q) = %q, want %q", c.args, got, c.want)
		}
	}
}
