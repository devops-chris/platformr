package config

import "strings"

// Resolve merges org-level and repo-level defaults into each resource's
// Resolved fields. Call this after loading a RepoConfig.
func Resolve(orgCfg *OrgConfig, repo *RepoConfig) {
	fieldLibrary := buildFieldLibrary(orgCfg.Defaults.Fields, repo.Defaults.Fields)

	for i := range repo.Resources {
		r := &repo.Resources[i]
		resolveFieldReferences(r.Fields, fieldLibrary)

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

		// Per-file target_path overrides can reference {{.resource}} too — apply the
		// same substitution here since these never go through the coalesce above.
		for j := range r.TemplateFiles {
			r.TemplateFiles[j].TargetPath = renderPattern(r.TemplateFiles[j].TargetPath, r.Name)
		}
	}
}

// buildFieldLibrary indexes org- and repo-level defaults.fields by Name, for
// resources to opt into by bare reference. Repo-level wins over org-level on a
// name collision, same precedence as every other repo-vs-org default.
func buildFieldLibrary(orgFields, repoFields []Field) map[string]Field {
	library := make(map[string]Field, len(orgFields)+len(repoFields))
	for _, f := range orgFields {
		library[f.Name] = f
	}
	for _, f := range repoFields {
		library[f.Name] = f
	}
	return library
}

// resolveFieldReferences replaces any field with no Type of its own by the
// matching library entry, in place, at whatever position the resource itself
// put it — this is what lets a resource decide where in its own field order a
// shared field (e.g. "vertical") gets evaluated, rather than always running
// before or after a resource's own fields. A field that already sets Type
// defines itself fully and is left untouched, even if the name also exists in
// the library. A bare name with no library match is left as-is too — it just
// behaves as a plain input field, same as it always has.
func resolveFieldReferences(fields []Field, library map[string]Field) {
	for i, f := range fields {
		if f.Type == "" {
			if def, ok := library[f.Name]; ok {
				fields[i] = def
			}
		}
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
