package config

// OrgConfig is fetched from github.com/<org>/.platformr/config.toml
// It is the entry point — it lists which IaC repos have platformr.toml files.
type OrgConfig struct {
	GitHub   OrgGitHubConfig  `toml:"github"`
	Defaults ResourceDefaults `toml:"defaults"`
	Repos    []RepoRef        `toml:"repos"`
	Branding BrandingConfig   `toml:"branding"`
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
	// Fields is a library of reusable field definitions, not fields every resource
	// automatically gets. A resource opts in by declaring a field with the same
	// Name and no Type of its own — see resolveFieldLibrary in resolver.go — at
	// whatever position in its OWN fields list it wants (this is what lets a
	// resource place e.g. "vertical" after "account", which its source path
	// depends on). A resource that never mentions the name is unaffected.
	Fields []Field `toml:"fields"`
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
	// RequestsDir is an optional folder of extra request files, each holding
	// [[resources]] (and optionally [maps]) exactly as they'd be written here.
	// Lets a repo with many requests keep one file per thing instead of one huge
	// platformr.toml. Defaults to DefaultRequestsDir; a missing folder is fine.
	RequestsDir string           `toml:"requests_dir"`
	Defaults    ResourceDefaults `toml:"defaults"`
	Resources   []Resource       `toml:"resources"`
	// Maps defines named lookup tables available in templates via {{index .maps.<name> <key>}}.
	// Useful for mapping computed values (e.g. account name → AWS account ID) without prompting.
	Maps map[string]map[string]string `toml:"maps"`
	// Set at load time from the repo URL, not from TOML.
	RepoOwner string `toml:"-"`
	RepoName  string `toml:"-"` // full "owner/repo"
	RepoRef   string `toml:"-"` // git ref this config was fetched from (empty = default branch)
}

type Resource struct {
	Name             string               `toml:"name"`
	DisplayName      string               `toml:"display_name"` // optional friendly name shown in picker and catalog; name is still used for CLI args
	Category         string               `toml:"category"`     // optional grouping label shown in picker and catalog
	Description      string               `toml:"description"`
	Template         string               `toml:"template"`           // path within this repo, e.g. "platformr/templates/service.yaml.tmpl"
	TemplateDir      string               `toml:"template_dir"`       // directory of .tmpl files — all rendered and committed (takes precedence over template)
	TargetOrg        string               `toml:"target_org"`         // override org for the PR target repo
	TargetRepo       string               `toml:"target_repo"`        // override repo for PRs (defaults to the repo this config lives in)
	TargetPath       string               `toml:"target_path"`        // replaces the default target_path entirely; supports {{.field}} interpolation
	TargetPathSuffix string               `toml:"target_path_suffix"` // appended to the default target_path instead of replacing it
	FileName         string               `toml:"file_name"`          // supports {{.field}} interpolation, e.g. "{{.vpc_name}}". defaults to first field value
	FileExt          string               `toml:"file_ext"`           // e.g. ".tf", ".tfvars", ".yaml". defaults to ".yaml"
	BaseBranch       string               `toml:"base_branch"`
	PRTitle          string               `toml:"pr_title"`
	Reviewers        []string             `toml:"reviewers"`      // GitHub usernames auto-requested on every PR for this resource
	TeamReviewers    []string             `toml:"team_reviewers"` // GitHub team slugs auto-requested on every PR for this resource
	Fields           []Field              `toml:"fields"`
	TemplateFiles    []TemplateFileConfig `toml:"template_files"` // per-file metadata for multi-file mode
	// OutputPath is a platform-team-asserted contract, not something derived from the
	// template: "once applied, outputs for this resource will be written under this path."
	// Supports {{.field}} interpolation like TargetPath. IaC-agnostic and backend-agnostic —
	// platformr only renders and displays the string, it never reads or writes anything there.
	// Absent means this resource doesn't have output support wired up.
	OutputPath string `toml:"output_path"`
	// OutputKeys names the individual keys written under OutputPath (e.g. "endpoint",
	// "port", "arn"), so platformr can display full per-key paths instead of just the
	// prefix. Same contract as OutputPath — the platform team asserts these are the
	// keys that will exist, platformr never verifies it. Ignored if OutputPath is unset.
	OutputKeys []string `toml:"output_keys"`
	// OutputCloud is metadata only — "aws", "azure", or "gcp", matching the same
	// Cloud convention used by the author's cloudctx tool. platformr doesn't act on
	// it today (nothing yet knows how to fetch values from more than one backend),
	// but recording it now means the schema won't need to change once lockr (or
	// whatever fetches values) actually supports more than AWS. Ignored if
	// OutputPath is unset.
	OutputCloud string `toml:"output_cloud"`
	// Instructions is free-text shown in its own section of the PR body — anything
	// the platform team wants whoever handles this next to know. Deliberately
	// mechanism-agnostic: platformr doesn't know or care whether that means running
	// a specific terragrunt/terraform command, waiting on a GitOps controller to
	// reconcile, getting a manual approval somewhere, or nothing at all. Supports
	// {{.field}} interpolation like OutputPath. Absent means no section is shown.
	Instructions string `toml:"instructions"`
	// UpdateFile makes this a change request: instead of rendering templates into new
	// files, it changes values inside this existing file (supports {{.field}}). Fields
	// with a `key` read their current value from it and write the answer back; Changes
	// add or remove items in lists and maps. See docs/changing-existing-resources.md.
	UpdateFile string `toml:"update_file"`
	// Format overrides how UpdateFile is read when its extension doesn't say:
	// yaml, json, hcl, env or marker.
	Format string `toml:"format"`
	// Changes are collection edits (append / remove / put / delete) and file deletions
	// made by a change request, in order, after the questions are answered.
	Changes []ChangeConfig `toml:"changes"`
	// Resolved is populated by the resolver after loading. Do not set in TOML.
	Resolved ResolvedResource `toml:"-"`
}

// Label returns the display name for use in pickers and catalog output.
// Falls back to Name if DisplayName is not set.
func (r Resource) Label() string {
	if r.DisplayName != "" {
		return r.DisplayName
	}
	return r.Name
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

// TemplateFileConfig holds per-file settings for multi-file template mode.
// Each entry matches a SOURCE .tmpl filename (minus the .tmpl suffix, unrendered) — not
// the rendered output name. Matching on the source name (which is unique on disk) rather
// than the rendered name is what lets multiple source files (e.g. "terragrunt.hcl.tmpl",
// "eip-terragrunt.hcl.tmpl", "twingate-terragrunt.hcl.tmpl") each get their own
// target_path/output_name override even when several of them need to render to the exact
// same final filename (e.g. "terragrunt.hcl", required by Terragrunt) in different
// directories — matching on the rendered name would make those indistinguishable.
type TemplateFileConfig struct {
	Name         string `toml:"name"`           // source filename minus .tmpl, e.g. "eip-terragrunt.hcl"
	TargetPath   string `toml:"target_path"`    // directory in target repo; overrides the resource-level target_path for this file
	OutputName   string `toml:"output_name"`    // overrides the rendered output filename (supports {{.field}} interpolation); use when the source filename can't just be the desired output name, e.g. multiple files that must all become "terragrunt.hcl"
	SkipIfExists string `toml:"skip_if_exists"` // path in target repo; skip this file if it already exists
}

type Field struct {
	Name  string `toml:"name"`
	Type  string `toml:"type"`  // "input", "select", "computed", "reviewer", "team_reviewer", or "file_lookup"
	Value string `toml:"value"` // Go template expression for computed fields
	Label string `toml:"label"`
	// Source is a dynamic-value directive whose meaning depends on Type:
	//   select:      "dirs:<path>", "files:<path>", "team:<slug>", or "collaborators"
	//   file_lookup: a file path in this repo, supports {{.field}} interpolation
	Source       string   `toml:"source"`
	AllowManual  bool     `toml:"allow_manual"` // offer "[+ enter manually]" alongside the listed options, or go straight to a text prompt if the list is empty. Doesn't create or validate anything — the typed value is used as-is.
	Options      []string `toml:"options"`      // static options for select
	Default      string   `toml:"default"`
	Validate     string   `toml:"validate"` // "unique" — checks target repo for conflicts
	Placeholder  string   `toml:"placeholder"`
	Optional     bool     `toml:"optional"`      // if true, field may be left blank; use {{if .field}} in templates
	StripPrefix  string   `toml:"strip_prefix"`  // remove this prefix from dynamically sourced option values
	FilterPrefix string   `toml:"filter_prefix"` // only include options that start with this prefix
	FilterSuffix string   `toml:"filter_suffix"` // only include options that end with this suffix
	StripSuffix  string   `toml:"strip_suffix"`  // remove this suffix from dynamically sourced option values
	Exclude      []string `toml:"exclude"`       // leave these out of the options (after stripping)
	When         string   `toml:"when"`          // Go template expression — field is skipped when result is not "true"
	// Pattern is a regex with exactly one capture group, used by type = "file_lookup"
	// to extract a value out of the file at Source. No prompt is shown; a fetch
	// failure or non-match is a hard error, since a silently empty/wrong value here
	// could quietly corrupt anything downstream that references this field.
	Pattern string `toml:"pattern"`
	// SearchParents makes a file_lookup look for Source's file name in Source's folder,
	// then each parent folder up to the repo root, and use the first one found — like
	// Terragrunt's find_in_parent_folders. For files such as env.hcl that sit at
	// different depths in different accounts.
	SearchParents bool `toml:"search_parents"`
	// Key points at a value in the request's update_file (or this field's own
	// update_file): the question starts on the current value there, and the answer is
	// written back to the same spot. A string is a dotted path ("resources.requests.cpu");
	// a list keeps each step whole and allows matches: ["Statement", {Sid = "S3Read"}, "Action"].
	// "marker:<name>" points at a line tagged with a platformr:<name> comment instead.
	Key any `toml:"key"`
	// UpdateFile / Format override the request's update_file for this one field.
	UpdateFile string `toml:"update_file"`
	Format     string `toml:"format"`
	// List fills a select with the items of a list or map in the update_file (same
	// path syntax as Key), e.g. the users that exist, for a "remove user" request.
	// Show names the field to display when the items are objects.
	List any    `toml:"list"`
	Show string `toml:"show"`
}

// ChangeConfig is one [[resources.changes]] entry. Every string supports {{.field}}.
type ChangeConfig struct {
	// Action: append (add to a list), remove (take matching items out of a list),
	// put (add a named entry to a map), delete (remove a named entry from a map),
	// set (change one value), or delete_file (remove a whole file, for
	// one-file-per-item collections).
	Action string `toml:"action"`
	Key    any    `toml:"key"`
	// Item is what append/put adds: a plain value ("{{.cidr}}") or an object
	// ({ name = "{{.username}}", role = "{{.role}}" }).
	Item any `toml:"item"`
	// Name is the map entry for put/delete.
	Name string `toml:"name"`
	// Value is the new value for set.
	Value string `toml:"value"`
	// Match picks what remove takes out: a plain value ("{{.cidr}}") or the fields an
	// object must have ({ name = "{{.username}}" }).
	Match any `toml:"match"`
	// UniqueBy stops an append if an item with the same value already exists: a field
	// name for objects, or "value" for plain lists.
	UniqueBy string `toml:"unique_by"`
	// File / Format override the request's update_file for this change. For
	// delete_file, File is the file to delete.
	File   string `toml:"file"`
	Format string `toml:"format"`
	// When skips this change unless it renders to "true", like a field's when.
	When string `toml:"when"`
	// IfExists decides what an append with unique_by does when the item is already
	// there: "error" (default) stops the request; "skip" leaves the list as it is.
	IfExists string `toml:"if_exists"`
}

// RendersTemplates reports whether this request creates files from templates. A
// plain create request does; a change request does only if it names templates
// itself (template, template_dir or template_files), so one request can create new
// files and edit existing ones in the same PR, e.g. a new account's folder plus its
// entry in the organization's account list.
func (r Resource) RendersTemplates() bool {
	return !r.IsUpdate() || r.Template != "" || r.TemplateDir != "" || len(r.TemplateFiles) > 0
}

// IsUpdate reports whether this is a change request (edits existing files) rather
// than a create request (renders templates into new files).
func (r Resource) IsUpdate() bool {
	if r.UpdateFile != "" || len(r.Changes) > 0 {
		return true
	}
	for _, f := range r.Fields {
		if f.Key != nil || f.List != nil {
			return true
		}
	}
	return false
}
