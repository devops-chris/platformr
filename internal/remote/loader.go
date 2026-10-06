package remote

import (
	"fmt"
	"os"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/devops-chris/platformr/internal/config"
	"github.com/devops-chris/platformr/internal/github"
)

// RefOverride, when set, overrides the configured ref/branch for every IaC repo,
// for this invocation only — the org config (shared by everyone) is untouched.
// Set via the `--ref` flag (cmd/root.go binds to this directly) or the
// PLATFORMR_REF env var if the flag isn't given.
var RefOverride string

// refFor resolves the ref to use for a repo: RefOverride wins, then PLATFORMR_REF,
// then the ref configured for that repo in the org config.
func refFor(configuredRef string) string {
	if RefOverride != "" {
		return RefOverride
	}
	if envRef := os.Getenv("PLATFORMR_REF"); envRef != "" {
		return envRef
	}
	return configuredRef
}

// Loader fetches and parses platformr configs from GitHub at runtime.
// No local config files are needed beyond the connected org name.
type Loader struct {
	gh *github.Client
	// Warnings collects config problems found by LoadAll that hid some requests but
	// didn't stop the others: a platformr.toml or request file that doesn't parse,
	// a duplicate name. Commands print them so a config mistake is never silent.
	Warnings []string
}

func New(token string) *Loader {
	return &Loader{gh: github.New(token)}
}

// LoadAll fetches the org config from <org>/.platformr/config.toml,
// then fetches platformr.toml from each registered IaC repo,
// resolves defaults into each resource, and returns everything.
func (l *Loader) LoadAll(orgName string) (*config.OrgConfig, []*config.RepoConfig, error) {
	orgCfg, err := l.loadOrgConfig(orgName)
	if err != nil {
		return nil, nil, fmt.Errorf("loading org config from %s/.platformr: %w", orgName, err)
	}

	var repos []*config.RepoConfig
	for _, repoRef := range orgCfg.Repos {
		repoURL := resolveRepoURL(repoRef.URL, orgCfg.GitHub.DefaultOrg)

		repoCfg, err := l.loadRepoConfig(repoURL, refFor(repoRef.Ref))
		if err != nil {
			// A repo without a platformr.toml just isn't set up yet — skip it quietly.
			// Anything else (usually a typo) hides all of its requests, so say so.
			if !github.IsNotFound(err) {
				l.Warnings = append(l.Warnings, fmt.Sprintf(
					"%s: platformr.toml couldn't be read, so none of its requests are shown. Fix it and try again.\n  %v", repoURL, err))
			}
			continue
		}
		l.loadRequestFiles(repoCfg)

		config.Resolve(orgCfg, repoCfg)
		repos = append(repos, repoCfg)
	}

	return orgCfg, repos, nil
}

// AllResources flattens resources across all repos into a single list.
func AllResources(repos []*config.RepoConfig) []config.Resource {
	var all []config.Resource
	for _, repo := range repos {
		all = append(all, repo.Resources...)
	}
	return all
}

// MapsFor returns the named maps defined in the repo that owns the given resource.
func MapsFor(resource config.Resource, repos []*config.RepoConfig) map[string]map[string]string {
	for _, repo := range repos {
		if repo.RepoName == resource.Resolved.TemplateRepo {
			return repo.Maps
		}
	}
	return nil
}

// FindResource finds a resource by name across all repos.
func FindResource(name string, repos []*config.RepoConfig) (config.Resource, bool) {
	for _, repo := range repos {
		for _, r := range repo.Resources {
			if r.Name == name {
				return r, true
			}
		}
	}
	return config.Resource{}, false
}

func (l *Loader) loadOrgConfig(org string) (*config.OrgConfig, error) {
	content, err := l.gh.FetchFile(org+"/.platformr", "config.toml", "")
	if err != nil {
		return nil, err
	}
	var cfg config.OrgConfig
	if _, err := toml.Decode(content, &cfg); err != nil {
		return nil, fmt.Errorf("parsing org config: %w", err)
	}
	return &cfg, nil
}

func (l *Loader) loadRepoConfig(repoURL, ref string) (*config.RepoConfig, error) {
	content, err := l.gh.FetchFile(repoURL, "platformr.toml", ref)
	if err != nil {
		return nil, err
	}
	var cfg config.RepoConfig
	if _, err := toml.Decode(content, &cfg); err != nil {
		return nil, fmt.Errorf("parsing repo config for %s: %w", repoURL, err)
	}

	parts := strings.SplitN(repoURL, "/", 2)
	cfg.RepoOwner = parts[0]
	cfg.RepoName = repoURL
	cfg.RepoRef = ref

	return &cfg, nil
}

// loadRequestFiles adds every *.toml in the repo's requests folder to repoCfg. The
// folder is optional. A file with a problem is skipped with a warning; the rest load.
func (l *Loader) loadRequestFiles(repoCfg *config.RepoConfig) {
	dir := repoCfg.RequestsDirPath()
	files, err := l.gh.FetchDirFiles(repoCfg.RepoName, dir, repoCfg.RepoRef, ".toml")
	if err != nil {
		if !github.IsNotFound(err) {
			l.Warnings = append(l.Warnings, fmt.Sprintf(
				"%s: couldn't read request files in %s, so they aren't shown.\n  %v", repoCfg.RepoName, dir, err))
		}
		return
	}
	byName := map[string]string{}
	var names []string
	for _, f := range files {
		byName[f.Name] = f.Content
		names = append(names, f.Name)
	}
	for _, name := range config.SortedFileNames(names) {
		path := dir + "/" + name
		if err := repoCfg.AddRequestFile(path, byName[name]); err != nil {
			l.Warnings = append(l.Warnings, fmt.Sprintf(
				"%s: skipped %s, so its requests aren't shown. Fix it and try again.\n  %v", repoCfg.RepoName, path, err))
		}
	}
}

// ResolveRepoURL expands a shorthand repo name to "org/repo" format.
func ResolveRepoURL(url, defaultOrg string) string {
	if strings.Contains(url, "/") {
		return url
	}
	return defaultOrg + "/" + url
}

func resolveRepoURL(url, defaultOrg string) string {
	return ResolveRepoURL(url, defaultOrg)
}
