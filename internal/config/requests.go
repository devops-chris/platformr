package config

import (
	"fmt"
	"sort"

	"github.com/BurntSushi/toml"
)

// DefaultRequestsDir is where extra request files live when platformr.toml
// doesn't set requests_dir.
const DefaultRequestsDir = "platformr/requests"

// requestFile is what a file in the requests folder may contain: the same
// [[resources]] and [maps] blocks platformr.toml uses, nothing else.
type requestFile struct {
	Resources []Resource                   `toml:"resources"`
	Maps      map[string]map[string]string `toml:"maps"`
}

// RequestsDirPath returns the folder to read extra request files from.
func (r *RepoConfig) RequestsDirPath() string {
	if r.RequestsDir != "" {
		return r.RequestsDir
	}
	return DefaultRequestsDir
}

// AddRequestFile parses one request file and adds its requests and maps to r.
// Nothing is added if the file has any problem, so a broken file only hides its
// own requests. Errors name the file and say what to fix.
func (r *RepoConfig) AddRequestFile(path, content string) error {
	var f requestFile
	md, err := toml.Decode(content, &f)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	for _, key := range []string{"defaults", "requests_dir"} {
		if md.IsDefined(key) {
			return fmt.Errorf("%s: %q can only be set in platformr.toml, not in a request file", path, key)
		}
	}
	if len(f.Resources) == 0 {
		return fmt.Errorf("%s: no [[resources]] found — a request file needs at least one", path)
	}

	existing := map[string]bool{}
	for _, res := range r.Resources {
		existing[res.Name] = true
	}
	for _, res := range f.Resources {
		if res.Name == "" {
			return fmt.Errorf("%s: a [[resources]] block has no name", path)
		}
		if existing[res.Name] {
			return fmt.Errorf("%s: a request named %q already exists (in platformr.toml or another request file) — names must be unique", path, res.Name)
		}
		existing[res.Name] = true
	}
	for name := range f.Maps {
		if _, ok := r.Maps[name]; ok {
			return fmt.Errorf("%s: a map named %q already exists (in platformr.toml or another request file) — map names must be unique", path, name)
		}
	}

	r.Resources = append(r.Resources, f.Resources...)
	if len(f.Maps) > 0 && r.Maps == nil {
		r.Maps = map[string]map[string]string{}
	}
	for name, m := range f.Maps {
		r.Maps[name] = m
	}
	return nil
}

// SortedFileNames returns names in a stable order so request files load the same
// way every time (the picker's order follows file order, then order within a file).
func SortedFileNames(names []string) []string {
	out := append([]string(nil), names...)
	sort.Strings(out)
	return out
}
