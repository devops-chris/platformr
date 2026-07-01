# Configuration Reference

platformr has three layers of configuration, each owned by a different party.

---

## 1. Local config — `~/.config/platformr/config.toml`

Written by `platformr connect`. You never edit this manually.

```toml
connected_org = "acme-corp"
```

That's it. Everything else is fetched from GitHub at runtime.

---

## 2. Org config — `<org>/.platformr/config.toml`

Lives in a repo named `.platformr` inside your GitHub org (e.g. `github.com/acme-corp/.platformr`).
The platform team owns this file. It tells platformr which IaC repos to discover resources from.

```toml
[github]
default_org   = "acme-corp"      # used when repo URLs don't include an org prefix
app_client_id = "Iv1.xxxx"       # GitHub App Client ID — public, not a secret

[defaults]
base_branch = "main"             # default PR base branch for all resources

[[repos]]
url = "platform-claims"          # shorthand — resolves to acme-corp/platform-claims

[[repos]]
url = "terraform-infra"          # resolves to acme-corp/terraform-infra

[[repos]]
url = "other-org/shared-infra"   # explicit — different org, same GitHub instance
```

### `ref` — fetch templates from a specific branch

Each repo entry accepts an optional `ref` field specifying the branch, tag, or SHA
to fetch `platformr.toml` and template files from. This is independent of the PR
base branch (`base_branch`) — `ref` controls where config is read from, not where
PRs target.

```toml
[[repos]]
url = "terraform-infra"
ref = "cml-platformr"   # fetch platformr.toml and templates from this branch
                        # PRs still open against base_branch ("main")
```

Useful for testing new templates before merging to main.

### Enterprise with multiple orgs

If your company uses GitHub Enterprise Cloud (or Server) with many orgs, `default_org`
is just the fallback for shorthand repo URLs. You can reference repos from any org
explicitly using the `other-org/repo` format:

```toml
[github]
default_org = "platform-team"

[[repos]]
url = "platform-team/crossplane-claims"

[[repos]]
url = "infra-team/terraform-modules"   # different org

[[repos]]
url = "data-team/data-infra"           # yet another org
```

For **GitHub Enterprise Server** (self-hosted), set `GH_HOST` before connecting:

```bash
export GH_HOST=github.mycompany.com
platformr connect acme-corp
```

---

## 3. Repo config — `platformr.toml` in each IaC repo

Lives in the root of any IaC repo listed in the org config.
The team that owns the IaC repo owns this file.
Resource definitions and templates live here alongside the IaC they describe.

### Single-file template mode

One template file renders to one committed file:

```toml
[defaults]
# {{.resource}} is replaced with the resource type name at load time.
# All other {{.field}} expressions are replaced with user input at request time.
target_path   = "claims/{{.resource}}/"
template_path = "platformr/templates/{{.resource}}.yaml.tmpl"
base_branch   = "main"

[[resources]]
name        = "service"
description = "Create a new microservice"
pr_title    = "feat(service): add {{.name}}"

  [[resources.fields]]
  name        = "name"
  type        = "input"
  label       = "Service name"
  placeholder = "payments-worker"
  validate    = "unique"
```

### Multi-file template mode

One request renders an entire directory of `.tmpl` files (e.g. multiple Terraform files).
Set `template_dir_path` instead of `template_path`:

```toml
[defaults]
target_path       = "cloud/aws/{{.account}}/{{.region}}/{{.resource}}/"
template_dir_path = "platformr/templates/{{.resource}}"
base_branch       = "main"
```

Every `.tmpl` file in the directory is fetched, rendered, and committed.
The output filename is the template name with `.tmpl` stripped
(e.g. `vpc.tf.tmpl` → `vpc.tf`).

### Templated output filenames

Template filenames themselves support `{{.field}}` expressions, rendered at
request time. This is useful when multiple deployments of the same resource
type land in the same directory and need unique filenames:

```
platformr/templates/eks/
├── eks-{{.name}}.tf.tmpl        → eks-services.tf
├── labels-{{.name}}.tf.tmpl     → labels-services.tf
└── terragrunt.hcl.tmpl          → terragrunt.hcl
```

Files without template expressions in the name are unaffected — `terragrunt.hcl.tmpl`
always outputs `terragrunt.hcl`.

### Extending the default path with a suffix

By default, setting `target_path` on a resource replaces the default entirely.
Use `target_path_suffix` instead to append to the default `target_path`:

```toml
[defaults]
target_path = "cloud/aws/{{.account}}/{{.region}}/"

[[resources]]
name              = "vpc"
# no override — uses default as-is

[[resources]]
name              = "project-file"
target_path_suffix = "{{.project}}/templates/"
# resolves to: cloud/aws/{{.account}}/{{.region}}/{{.project}}/templates/

[[resources]]
name        = "namespace-template"
target_path = "namespace1/templates/"
# replaces default entirely — static path
```

`target_path_suffix` supports the same `{{.field}}` expressions as `target_path`.
If both are set, `target_path` takes precedence.

### Template conditionals in paths

`target_path` and `pr_title` support full Go `text/template` syntax, including
conditionals. This is useful when path structure varies by field value:

```toml
# Prod accounts don't have an environment subdirectory in the path,
# but "prod" is still passed to Terraform as var.environment for labeling.
target_path = 'cloud/aws/{{.account}}/{{if ne .environment "prod"}}{{.environment}}/{{end}}{{.region}}/{{.resource}}/'
pr_title    = 'feat(vpc): add {{.name}} in {{.account}}/{{if ne .environment "prod"}}{{.environment}}/{{end}}{{.region}}'
```

> **Note:** Use TOML literal strings (single quotes `'...'`) when your template
> contains double quotes. Literal strings are not processed for escape sequences.

---

## Resource categories

Resources can be grouped with `category` for a tidier picker and catalog:

```toml
[[resources]]
name        = "vpc"
category    = "Infrastructure"
description = "Request a new VPC"

[[resources]]
name        = "scale-nodes"
category    = "2nd Day Operations"
description = "Scale a node group"
```

Resources without `category` are grouped under **General**.
Both `platformr request` and `platformr catalog` group and label by category.

---

## Reviewers & PR comments

### Auto-reviewers (config-driven)

Add `reviewers` and/or `team_reviewers` to a resource definition to automatically
request review on every PR for that resource type. Developers are never prompted —
the assignment happens silently when the PR is created.

```toml
[[resources]]
name           = "eks"
description    = "Request a new EKS cluster"
reviewers      = ["alice"]          # GitHub usernames
team_reviewers = ["platform-team"]  # GitHub team slugs
```

### Selectable reviewers (developer-chosen)

For cases where the developer needs to tag someone they're working with, add a
field with `type = "reviewer"` or `type = "team_reviewer"`. It renders as a
select during the request flow, and the chosen value is added to the PR's reviewer
list in addition to any config-driven reviewers above.

The options list can be static or dynamically sourced:

```toml
# Static list
[[resources.fields]]
name     = "reviewer"
type     = "reviewer"
label    = "Tag someone to review with? (optional)"
options  = ["alice", "bob", "carol"]
optional = true

# Dynamic — fetched from a GitHub team at request time
[[resources.fields]]
name     = "reviewer"
type     = "reviewer"
label    = "Tag someone to review with? (optional)"
source   = "team:devops-team"   # slug of a team in your org
optional = true

# Dynamic — fetched from the PR target repo's collaborators
[[resources.fields]]
name     = "reviewer"
type     = "reviewer"
label    = "Tag someone to review with? (optional)"
source   = "collaborators"
optional = true
```

Use `type = "team_reviewer"` to assign a GitHub team instead of an individual.

The selected value is also available as `{{.reviewer}}` in templates if needed.

### PR comments

At the end of every `platformr request` flow, developers are prompted for optional
freeform notes. Pressing Enter skips it. If provided, the text is appended to the
PR body under a **Notes** heading:

```
### Notes

needs to land before the RDS migration on Friday
```

No configuration required — this prompt appears on all resource types.

---

## Fields

### Field types

| type | behaviour |
|---|---|
| `input` | Free-text input. Supports `default`, `placeholder`, `validate`, and `optional`. |
| `select` | Dropdown. Populated from `options` (static) or `source` (dynamic). Supports `optional`. |

### Optional fields

Mark a field `optional = true` to allow it to be left blank. Input fields show
an `(optional)` label; select fields show a `— skip —` option at the top.
Use `{{if .field}}...{{end}}` in templates to omit blocks when the field is empty:

```toml
[[resources.fields]]
name     = "annotations"
type     = "input"
label    = "Extra annotations"
optional = true
```

```hcl
# In the template — block only appears if annotations was filled in
{{if .annotations}}
  annotations:
    note: "{{.annotations}}"
{{end}}
```

### Input defaults and placeholders

If `default` is set, the input is pre-filled with that value.
If only `placeholder` is set (no `default`), the input is pre-filled with the
placeholder — the user can accept it by pressing Enter or type to replace it.

```toml
[[resources.fields]]
name        = "cidr"
type        = "input"
label       = "VPC CIDR block"
placeholder = "10.0.0.0/16"   # pre-filled; press Enter to accept
```

### Static select

```toml
[[resources.fields]]
name    = "environment"
type    = "select"
label   = "Environment"
options = ["dev", "stg", "prod"]
```

### Dynamic select — `source = "resource.<type>"`

Reads existing deployed resources of another type from GitHub and presents them
as options. Useful for dependencies (e.g. pick an existing VPC before creating an RDS).

```toml
[[resources.fields]]
name         = "vpc"
type         = "select"
source       = "resource.vpc"   # lists files in vpc resource's target_path
allow_create = true             # adds "[+ create new]" — runs the vpc request flow inline
```

### Stripping prefixes from dynamic sources

When directory or file names include a structural prefix that shouldn't be exposed
to the developer or used in templates, use `strip_prefix` to remove it from the
displayed options:

```toml
[[resources.fields]]
name         = "project"
type         = "select"
source       = "dirs:cloud/aws/my-cluster/namespaces"
strip_prefix = "platform-"   # dirs are "platform-foo", "platform-bar" — shown as "foo", "bar"
label        = "Project"
```

The stripped value is what gets stored and used in `{{.project}}` template expressions.
To reconstruct the original dir name in `target_path_suffix`, prepend the prefix back:

```toml
target_path_suffix = "platform-{{.project}}/"
```

`strip_prefix` applies to `dirs:`, `resource.<type>`, `team:`, and `collaborators` sources.

### Dynamic select — `source = "dirs:<path>"`

Lists subdirectory names at a static path in the IaC repo at request time.
Use this for fields whose options are directories in the repo (e.g. account names).

```toml
[[resources.fields]]
name   = "account"
type   = "select"
label  = "AWS account"
source = "dirs:cloud/aws"   # lists subdirectories of cloud/aws/ at request time
```

The directory list is fetched live from the same branch the templates are read from
(controlled by `ref` in the org config).

### Field validation

```toml
validate = "unique"   # checks that no file named <value>.yaml exists at target_path
```

platformr checks the target repo before confirming — exits with an error if a
conflict is found.

---

## Template variables

Templates use standard Go `text/template` syntax. All field values are available
by name. Conditionals, comparisons, and the full template stdlib are supported.

```hcl
# platformr/templates/vpc/vpc.tf.tmpl
module "vpc_{{.name}}" {
  source      = "terraform-aws-modules/vpc/aws"
  name        = "{{.name}}"
  cidr        = "{{.cidr}}"
  environment = "{{.environment}}"
}
```

---

## Repo layout convention

```
your-org/terraform-infra/
├── platformr.toml                        ← resource definitions
├── platformr/
│   └── templates/
│       ├── vpc/                          ← multi-file template dir
│       │   ├── vpc.tf.tmpl
│       │   ├── labels.tf.tmpl
│       │   └── terragrunt.hcl.tmpl
│       └── service.yaml.tmpl             ← single-file template
└── cloud/
    └── aws/                              ← where PRs land
        └── my-account/
            └── use1/
                └── vpc/
```

---

## Auth

platformr uses separate tokens for read and write operations:

**Read operations** (fetching config, templates, directory listings) use:
1. `GITHUB_TOKEN` environment variable
2. `GH_TOKEN` environment variable
3. `gh auth token` (GitHub CLI)

**Write operations** (creating branches and PRs) use:
1. App token stored by `platformr auth` (OS keychain)
2. Falls back to the read token sources above

For most developers who already use `gh`, no token setup is needed — `gh auth token`
is used for everything. Run `platformr auth` only if you want PRs attributed to the
GitHub App instead of your personal account.
