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

### Overriding ref at runtime

The `ref` above is set by the platform team in the org config and applies to
*everyone* who runs platformr. To test in-progress IaC changes on a branch
yourself — without changing what anyone else sees — override it for just your
own invocation:

```bash
platformr --ref my-test-branch request rds
# or
PLATFORMR_REF=my-test-branch platformr request rds
```

This overrides the ref used for every configured repo for that one run. Precedence:
`--ref` flag > `PLATFORMR_REF` env var > the `ref` configured in the org config.
Nobody else's runs are affected — the org config itself is untouched.

### Branding

Orgs can override the banner name and description shown when running `connect`
and on subsequent command invocations. This is useful when distributing the
binary under a custom name (e.g. via a private Homebrew tap):

```toml
[branding]
name        = "pt-platform"
description = "PracticeTek developer self-service CLI"
```

The branding is fetched once during `connect` and cached in
`~/.config/platformr/config.toml`. The binary name (`os.Args[0]`) is used as a
fallback when `name` is not set, so re-running `connect` without a `[branding]`
section always shows the right executable name.

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

### Per-file target paths and conditional skipping

By default every file in a `template_dir` is committed to the same `target_path`
directory. Use `[[resources.template_files]]` to override this per file — useful
when a single request needs to write to multiple locations (e.g. the main resource
plus shared account-level infrastructure):

```toml
[[resources]]
name         = "ecr"
template_dir = "platformr/templates/ecr"

  # ecr.tf lands at the default target_path (per-repo subdirectory)

  # oidc.tf is account-level — route it to global/oidc/ and skip if already there
  [[resources.template_files]]
  name           = "oidc.tf"
  target_path    = "cloud/aws/{{.account}}/global/oidc/"
  skip_if_exists = "cloud/aws/{{.account}}/global/oidc/oidc.tf"

  # iam-ecr-push.tf routes to global/iam/ and is skipped if already present
  [[resources.template_files]]
  name           = "iam-ecr-push.tf"
  target_path    = "cloud/aws/{{.account}}/global/iam/"
  skip_if_exists = "cloud/aws/{{.account}}/global/iam/iam-ecr-push.tf"
```

**`target_path`** overrides the resource-level `target_path` for that specific file.
Files without an entry use the default. Both fields support `{{.field}}` expressions.

**`skip_if_exists`** checks whether a path already exists in the target repo before
committing that file. If it does, the file is omitted from the PR silently (a note
is printed). The other files still render and commit normally.

If every file in the directory is skipped, platformr exits with a message rather
than opening an empty PR.

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

### `output_path` — where applied outputs will land

platformr opens the PR; it doesn't apply anything or know what your CI/CD,
Atlantis, or a human running `terraform apply` produces afterward. If a
resource has outputs worth surfacing later (an RDS endpoint, an ARN, etc.),
set `output_path` to tell platformr where to point developers once it's
applied:

```toml
[[resources]]
name = "rds"
...
output_path = "cloud/aws/{{.account}}/{{.environment}}/{{.region}}/{{.resource}}/{{.name}}/"
```

Supports the same `{{.field}}` interpolation as `target_path`. platformr only
renders and **displays** this string after opening the PR — it never reads or
writes anything there itself, and has no opinion on what backend or IaC tool
you use to actually populate it (SSM, Vault, or anything else; Terraform,
Crossplane, or anything else). It's a contract the platform team asserts, not
something platformr can verify or derive from the template — leave it unset
for resources that don't have outputs wired up yet.

A bare prefix isn't very actionable on its own — you'd be guessing at key
names. Add `output_keys` to name the individual keys that will exist under
that prefix (e.g. an RDS endpoint, port, and ARN):

```toml
output_path = "cloud/aws/{{.account}}/{{.environment}}/{{.region}}/{{.resource}}/{{.name}}/"
output_keys = ["endpoint", "port", "arn"]
```

platformr then prints the full path per key instead of just the prefix:

```
Once applied, outputs should be written to:
    endpoint       cloud/aws/pt-chiro-ct-stg-services/dev/use1/rds/my-service/endpoint
    port           cloud/aws/pt-chiro-ct-stg-services/dev/use1/rds/my-service/port
    arn            cloud/aws/pt-chiro-ct-stg-services/dev/use1/rds/my-service/arn
```

Same contract as `output_path` — the platform team asserts these are the keys
that will exist; platformr never verifies it against what's actually written.
If a module's outputs change, keeping this list in sync is on the same team
that already owns making `output_path` true. `output_keys` is ignored if
`output_path` isn't set.

### `output_cloud` — which platform the values live in

Optional metadata: `"aws"`, `"azure"`, or `"gcp"` — the same `Cloud` convention
used by [cloudctx](https://github.com/devops-chris/cloudctx), rather than a
new one invented here.

```toml
output_cloud = "aws"
```

platformr doesn't act on this today — nothing yet knows how to fetch a value
from more than one backend. It's recorded now so the schema won't need to
change later, once something (`lockr` or otherwise) actually supports more
than AWS. Shown alongside the outputs in `platformr status`. Ignored if
`output_path` isn't set.

---

## Resource display names

By default the picker and catalog show the resource `name` — the same slug used
for `platformr request <name>`. Set `display_name` for a friendlier label without
changing the CLI argument:

```toml
[[resources]]
name         = "platform-project"
display_name = "Platform Project"
description  = "Create a new platform project"
```

- `platformr request platform-project` still works — `name` is the CLI slug
- The picker shows **Platform Project** and `platformr catalog` shows
  **Platform Project** `(platform-project)` so the slug is still discoverable
- Falls back to `name` when `display_name` is not set

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
| `select` | Dropdown. Populated from `options` (static) or `source` (dynamic). Supports `optional`, `allow_create`. |
| `computed` | No prompt — derives its value from a Go template expression. See [Computed fields](#computed-fields). |
| `reviewer` / `team_reviewer` | Like `select`, but the chosen value is also added to the PR's reviewers. See [Selectable reviewers](#selectable-reviewers-developer-chosen). |

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

`strip_prefix` applies to `dirs:`, `files:`, `team:`, and `collaborators` sources.

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

The path supports `{{.field}}` expressions referencing **previously collected fields**,
enabling dependent (chained) selects:

```toml
[[resources.fields]]
name   = "account"
type   = "select"
source = "dirs:cloud/aws"

[[resources.fields]]
name   = "region"
type   = "select"
options = ["use1", "usw2"]

[[resources.fields]]
name   = "cluster"
type   = "select"
source = "dirs:cloud/aws/{{.account}}/{{.region}}"   # resolved using prior answers

[[resources.fields]]
name         = "project"
type         = "select"
source       = "dirs:cloud/aws/{{.account}}/{{.region}}/{{.cluster}}"
strip_prefix = "platform-"
```

Fields are resolved in order — a `{{.field}}` expression is only valid if that field
appears earlier in the list.

### Dynamic select — `source = "files:<path>"`

Same as `dirs:`, but for resource types committed as **one flat file per
instance** instead of one directory per instance — i.e. resources using
`template_path` (single-file mode) rather than `template_dir_path`. Lists file
names at the given path, with the extension stripped:

```toml
# A "project" is one file: claims/project/payments.yaml
[[resources.fields]]
name   = "project"
type   = "select"
label  = "Which project is this service part of?"
source = "files:claims/project"   # → lists claims/project/*, minus extension
```

Use `dirs:` when instances are directories, `files:` when instances are flat
files. Both only ever look inside **the same repo** the current `platformr.toml`
lives in — neither can list resources defined in a different IaC repo, even one
also registered in the org config.

### `allow_create` — let the developer type a value instead of picking one

Add `allow_create = true` to any `dirs:`/`files:`-sourced select field to offer
a way out when the list doesn't have what they need — a value from an older
naming convention, something created by hand, or a resource type this repo just
doesn't track the way `dirs:`/`files:` expects:

```toml
[[resources.fields]]
name         = "vpc_name"
type         = "select"
label        = "VPC this cluster lives in"
source       = "dirs:cloud/aws/{{.account}}/{{.region}}/vpc"
allow_create = true
```

- If the list has existing options, `[+ create new]` is added at the bottom.
  Picking it re-prompts as a plain text box.
- If the list is empty, there's nothing to pick from — platformr skips the
  select entirely and goes straight to that same text box.

Either way, whatever gets typed is used as-is: **no validation that it matches
anything real, and nothing gets created.** `allow_create` only decides which
input widget to show; it has no side effects. If a field genuinely needs "the
value must be a real, existing thing," pair it with a template check on the
receiving end, or leave `allow_create` off so the developer can only pick from
what's actually there.

### Computed fields

Use `type = "computed"` to derive a field value from a template expression without
prompting the user. The expression is evaluated against already-collected field values
and stored under the field name for use in `target_path`, `pr_title`, and templates.

```toml
[[resources.fields]]
name  = "account"
type  = "computed"
value = '{{if eq .environment "prod"}}pt-prod-account{{else}}pt-nonprod-account{{end}}'

[[resources.fields]]
name  = "cluster"
type  = "computed"
value = '{{if eq .environment "dev"}}pt-dev-eks{{else if eq .environment "stg"}}pt-stg-eks{{else}}pt-prod-eks{{end}}'
```

The computed values are then available as `{{.account}}` and `{{.cluster}}` everywhere:

```toml
target_path = "cloud/aws/{{.account}}/use1/{{.cluster}}/"
```

Computed fields must appear **after** the fields they reference — `value` can only
use fields already collected earlier in the list. They respect `when` too, so you
can conditionally skip a computed field just like any other.

---

### Named maps

For larger value lookups — like mapping account names to AWS account IDs across
20+ accounts — define a `[maps]` section in `platformr.toml`. Each key under
`[maps]` is a named table available in templates via Go's built-in `index` function.

```toml
[maps.aws_account_ids]
pt-shared-dev-services  = "203918879355"
pt-shared-stg-services  = "203918879355"
pt-shared-prod-services = "444455556666"
pt-chiro-dev-services   = "555566667777"
pt-rcm-dev-services     = "666677778888"
# add more without touching resource or field definitions
```

Reference a map in a computed field using `{{index .maps.<name> <key>}}`:

```toml
[[resources.fields]]
name  = "aws_account_id"
type  = "computed"
value = '{{index .maps.aws_account_ids .account}}'
```

The lookup key is any already-collected field value — typically a computed field
like `account` that was resolved earlier in the list. Maps are also available
inside `.tmpl` files and anywhere else `{{.field}}` expressions are supported.

If the key is not found the value is empty, same as `missingkey=zero` behaviour
on other fields.

---

### Conditional fields

Use `when` to show a field only when a previous field matches a condition.
The value is a Go template expression evaluated against already-collected field
values — if it renders to `"true"` the field is shown; otherwise it is skipped
and its value is set to `""`.

```toml
[[resources.fields]]
name     = "create_repo"
type     = "select"
options  = ["true", "false"]
when     = '{{eq .environment "dev"}}'   # only shown when environment = dev

[[resources.fields]]
name     = "team_ids"
type     = "input"
optional = true
when     = '{{ne .environment "prod"}}'  # hidden for prod
```

Fields must appear **after** the fields they reference — `when` can only use
values already collected earlier in the list.

Skipped fields have an empty value in the template. Use `{{if .field}}` to
conditionally render blocks that depend on them:

```yaml
{{if .create_repo}}
  createRepo: {{.create_repo}}
{{end}}
```

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

## Template functions

In addition to standard Go `text/template` syntax, platformr provides the
following helper functions for use in `.tmpl` files:

| Function | Signature | Example |
|---|---|---|
| `split` | `split sep str` | split comma-separated team IDs into a YAML list |
| `trimPrefix` | `trimPrefix prefix str` | strip a known prefix from a value |
| `trimSuffix` | `trimSuffix suffix str` | strip a known suffix from a value |
| `toLower` | `toLower str` | normalize user input to lowercase |
| `toUpper` | `toUpper str` | normalize to uppercase |
| `contains` | `contains substr str` | conditional block based on value content |
| `replace` | `replace old new str` | substitute characters (e.g. `-` → `_`) |

### Examples

**Render a comma-separated input as a YAML list** — useful for `teamIds` or any
multi-value field collected as a single comma-separated string:

```yaml
# field: team_ids = "DevOps,my-team"
{{if .team_ids}}
  teamIds:
{{- range (split "," .team_ids)}}
    - {{.}}
{{- end}}
{{end}}
```

**Normalize a service name for use as a Kubernetes label** (labels must be
lowercase and cannot contain spaces):

```yaml
  labels:
    app: {{toLower .service_name}}
```

**Strip a prefix to get a short identifier**:

```yaml
# .project = "pt-payments" — strip "pt-" to get "payments"
  shortName: {{trimPrefix "pt-" .project}}
```

**Replace hyphens with underscores** for environment variable names:

```yaml
  env:
    - name: {{replace "-" "_" (toUpper .service_name)}}_PORT
      value: "8080"
```

**Conditional block based on whether a value contains a substring**:

```yaml
{{if contains "prod" .cluster}}
  replicaCount: 3
{{else}}
  replicaCount: 1
{{end}}
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
