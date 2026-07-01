package config

// OrgConfig is fetched from github.com/<org>/.platformr/config.toml
// It is the entry point — it lists which IaC repos have platformr.toml files.
type OrgConfig struct {
	GitHub   OrgGitHubConfig  `toml:"github"`
	Defaults ResourceDefaults `toml:"defaults"`
	Repos    []RepoRef        `toml:"repos"`
}

type OrgGitHubConfig struct {
	DefaultOrg  string `toml:"default_org"`
	AppClientID string `toml:"app_client_id"` // GitHub App Client ID — public, not a secret
}

// ResourceDefaults are the fallback values applied to any resource that
// does not explicitly set target_path, template, or base_branch.
type ResourceDefaults struct {
	// TargetPath supports {{.resource}} interpolation, e.g. "claims/{{.resource}}/"
	TargetPath string `toml:"target_path"`
	// TemplatePath supports {{.resource}} interpolation — single file mode.
	TemplatePath string `toml:"template_path"`
	// TemplateDirPath supports {{.resource}} interpolation — multi-file mode.
	// Every .tmpl file in this directory is rendered and committed.
	// Takes precedence over template_path when both are set.
	TemplateDirPath string `toml:"template_dir_path"`
	BaseBranch      string `toml:"base_branch"`
}

type RepoRef struct {
	// URL is either "repo-name" (uses default_org) or "other-org/repo-name"
	URL string `toml:"url"`
	// Ref is the branch/tag/SHA to fetch platformr.toml and templates from.
	// Does not affect which branch PRs target — that is controlled by base_branch.
	// Defaults to the repo's default branch when empty.
	Ref string `toml:"ref"`
}

// RepoConfig is fetched from each IaC repo's platformr.toml.
// Templates and resource definitions live alongside the IaC they describe.
type RepoConfig struct {
	Defaults  ResourceDefaults `toml:"defaults"`
	Resources []Resource       `toml:"resources"`
	// Set at load time from the repo URL, not from TOML.
	RepoOwner string `toml:"-"`
	RepoName  string `toml:"-"` // full "owner/repo"
	RepoRef   string `toml:"-"` // git ref this config was fetched from (empty = default branch)
}

type Resource struct {
	Name        string  `toml:"name"`
	Description string  `toml:"description"`
	Template    string  `toml:"template"`     // path within this repo, e.g. "platformr/templates/service.yaml.tmpl"
	TemplateDir string  `toml:"template_dir"` // directory of .tmpl files — all rendered and committed (takes precedence over template)
	TargetOrg   string  `toml:"target_org"`   // override org for the PR target repo
	TargetRepo  string  `toml:"target_repo"`  // override repo for PRs (defaults to the repo this config lives in)
	TargetPath  string  `toml:"target_path"`  // supports {{.field}} interpolation, e.g. "environments/{{.environment}}/"
	FileName    string  `toml:"file_name"`    // supports {{.field}} interpolation, e.g. "{{.vpc_name}}". defaults to first field value
	FileExt     string  `toml:"file_ext"`     // e.g. ".tf", ".tfvars", ".yaml". defaults to ".yaml"
	BaseBranch  string  `toml:"base_branch"`
	PRTitle     string  `toml:"pr_title"`
	Fields      []Field `toml:"fields"`
	// Resolved is populated by the resolver after loading. Do not set in TOML.
	Resolved ResolvedResource `toml:"-"`
}

// ResolvedResource holds the fully-resolved coordinates after merging defaults.
type ResolvedResource struct {
	Org          string // GitHub org owning the PR target repo
	Repo         string // full "org/repo" for PR target
	TargetPath   string // path within target repo where the file lands
	Template     string // path within source repo to fetch the template from (single-file mode)
	TemplateDir  string // path within source repo to a directory of .tmpl files (multi-file mode)
	TemplateRepo string // full "org/repo" where the template lives (the IaC repo)
	TemplateRef  string // git ref to fetch templates from (empty = repo default branch)
	BaseBranch   string
}

type Field struct {
	Name        string   `toml:"name"`
	Type        string   `toml:"type"`         // "input" or "select"
	Label       string   `toml:"label"`
	Source      string   `toml:"source"`       // "resource.<type>" — discovers existing resources
	AllowCreate bool     `toml:"allow_create"` // offer "[+ create new]" when sourcing from another resource
	Options     []string `toml:"options"`      // static options for select
	Default     string   `toml:"default"`
	Validate    string   `toml:"validate"`     // "unique" — checks target repo for conflicts
	Placeholder string   `toml:"placeholder"`
}
