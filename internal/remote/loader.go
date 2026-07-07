package remote

import (
	"fmt"
	"strings"

	"github.com/BurntSushi/toml"
	"github.com/devops-chris/platformr/internal/config"
	"github.com/devops-chris/platformr/internal/github"
)

// Loader fetches and parses platformr configs from GitHub at runtime.
// No local config files are needed beyond the connected org name.
type Loader struct {
	gh *github.Client
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

		repoCfg, err := l.loadRepoConfig(repoURL, repoRef.Ref)
		if err != nil {
			// Non-fatal: repo may not have a platformr.toml yet
			continue
		}

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
