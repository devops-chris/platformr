package config

// CategoryConfig is one [[categories]] entry in platformr.toml: the menu's order, and a
// one-line hint shown next to the category name.
type CategoryConfig struct {
	Name        string `toml:"name"`
	Description string `toml:"description"`
}

// Uncategorized is the menu group for requests without a category.
const Uncategorized = "General"

// IsChange reports whether the request changes something that already exists
// (action = "change") rather than creating something new (the default).
func (r Resource) IsChange() bool { return r.Action == "change" }

// CategoryOf returns the menu group a request belongs to.
func CategoryOf(r Resource) string {
	if r.Category == "" {
		return Uncategorized
	}
	return r.Category
}

// MenuCategories returns the categories that have at least one request, in menu order:
// first those listed in [[categories]] (in that order, with their descriptions), then any
// others in the order their requests appear, then the uncategorized group.
func MenuCategories(resources []Resource, declared []CategoryConfig) []CategoryConfig {
	used := map[string]bool{}
	for _, r := range resources {
		used[CategoryOf(r)] = true
	}
	var out []CategoryConfig
	seen := map[string]bool{}
	for _, c := range declared {
		if used[c.Name] && !seen[c.Name] {
			out = append(out, c)
			seen[c.Name] = true
		}
	}
	for _, r := range resources {
		if c := CategoryOf(r); c != Uncategorized && !seen[c] {
			out = append(out, CategoryConfig{Name: c})
			seen[c] = true
		}
	}
	if used[Uncategorized] && !seen[Uncategorized] {
		out = append(out, CategoryConfig{Name: Uncategorized})
	}
	return out
}

// InMenuOrder returns the requests in one category: creates first, then changes, each
// in the order they're defined.
func InMenuOrder(resources []Resource, category string) (creates, changes []Resource) {
	for _, r := range resources {
		if CategoryOf(r) != category {
			continue
		}
		if r.IsChange() {
			changes = append(changes, r)
		} else {
			creates = append(creates, r)
		}
	}
	return creates, changes
}
