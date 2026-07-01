package config

import "strings"

// Resolve merges org-level and repo-level defaults into each resource's
// Resolved fields. Call this after loading a RepoConfig.
func Resolve(orgCfg *OrgConfig, repo *RepoConfig) {
	for i := range repo.Resources {
		r := &repo.Resources[i]

		// Org: resource override → org default
		org := coalesce(r.TargetOrg, orgCfg.GitHub.DefaultOrg)
		r.Resolved.Org = org

		// PR target repo: resource override → the repo this config came from
		targetRepo := r.TargetRepo
		if targetRepo == "" {
			targetRepo = repo.RepoName
		}
		if !strings.Contains(targetRepo, "/") {
			targetRepo = org + "/" + targetRepo
		}
		r.Resolved.Repo = targetRepo

		// Target path: resource override → suffix appended to default → default alone
		var targetPath string
		if r.TargetPath != "" {
			targetPath = r.TargetPath
		} else if r.TargetPathSuffix != "" {
			base := coalesce(repo.Defaults.TargetPath, orgCfg.Defaults.TargetPath)
			targetPath = base + r.TargetPathSuffix
		} else {
			targetPath = coalesce(repo.Defaults.TargetPath, orgCfg.Defaults.TargetPath)
		}
		r.Resolved.TargetPath = renderPattern(targetPath, r.Name)

		// Template dir (multi-file) takes precedence over single-file template.
		tmplDir := coalesce(r.TemplateDir, repo.Defaults.TemplateDirPath, orgCfg.Defaults.TemplateDirPath)
		r.Resolved.TemplateDir = renderPattern(tmplDir, r.Name)

		// Template path (single-file): resource → repo default → org default
		if r.Resolved.TemplateDir == "" {
			tmplPath := coalesce(r.Template, repo.Defaults.TemplatePath, orgCfg.Defaults.TemplatePath)
			r.Resolved.Template = renderPattern(tmplPath, r.Name)
		}

		// Template lives in the IaC repo (the one that owns this platformr.toml)
		r.Resolved.TemplateRepo = repo.RepoName
		r.Resolved.TemplateRef = repo.RepoRef

		// Base branch
		r.Resolved.BaseBranch = coalesce(r.BaseBranch, repo.Defaults.BaseBranch, orgCfg.Defaults.BaseBranch, "main")
	}
}

func coalesce(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// renderPattern replaces {{.resource}} in a pattern with the resource name.
// All other {{.field}} expressions are left intact for rendering at request time.
func renderPattern(pattern, resourceName string) string {
	return strings.ReplaceAll(pattern, "{{.resource}}", resourceName)
}
