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
The platform team owns this file. It tells platformr which IaC repos to discover resources
from, and holds the request menu, since the menu spans all of them.

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

# The request menu: its order and a one-line hint per category.
# See "The request menu" below.
[[categories]]
name        = "Network"
description = "VPC and remote access into it"
```

### Where everything lives

| What | Where | Owned by |
|---|---|---|
| Which repos platformr reads, the request menu | org config: `<org>/.platformr/config.toml` | platform team |
| Shared settings for one repo: defaults, reusable fields, folders | that repo's `platformr.toml` | that repo's owners |
| Requests | `platformr/requests/*.toml` in each repo | that repo's owners |
| Templates | `platformr/templates/<request name>/` in each repo | that repo's owners |

A request names its menu group with `category = "..."`; the group's order and hint
come from the org config. A request in one repo can read facts from another
connected repo with [`repo`](#repo-read-from-another-repo-the-repos-are-your-catalog).

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

### Where templates and requests live

Two folders, both set at the top of `platformr.toml`, both optional:

```toml
requests_dir  = "platformr/requests"    # default: every *.toml here is a request file
templates_dir = "platformr/templates"   # default: each request's templates are in <templates_dir>/<request name>/

[defaults]
# {{.resource}} is replaced with the request name at load time.
# All other {{.field}} expressions are replaced with user input at request time.
target_path = "cloud/aws/{{.account}}/{{.region}}/{{.resource}}/"
base_branch = "main"

[[resources]]
name        = "vpc"                     # templates: platformr/templates/vpc/
description = "Request a new VPC"
```

Every `.tmpl` file in a request's folder is fetched, rendered, and committed.
The output filename is the template name with `.tmpl` stripped
(`vpc.tf.tmpl` → `vpc.tf`). One template in the folder means one committed file.

- **A request can use a different folder** with `template_dir = "some/other/folder"`.
- **Change-only requests** (ones with `update_file` and no templates of their own)
  don't need a folder.
- Both settings are plain folder paths. platformr adds the request name itself.

!!! note "Older settings"
    `[defaults] template_dir_path` (a pattern like `"platformr/templates/{{.resource}}"`)
    and `[defaults] template_path` (one template file per request) still work.
    `templates_dir` replaces them. Setting both the old and new names is an error,
    so there's only ever one answer to "where are the templates?".

### Splitting requests into separate files

A small repo can keep everything in `platformr.toml`. Once there are many requests,
put them in separate files instead: **`platformr.toml` stays at the repo root** and
keeps the shared settings, and each file in `platformr/requests/` holds the
requests for one thing:

```
platformr.toml              shared: [defaults], reusable fields, shared [maps]
platformr/requests/
├── database.toml           [[resources]] for "New database", "Resize database"
└── eks.toml                [[resources]] for "New cluster", "Upgrade cluster version"
platformr/templates/...     unchanged
```

A request file is written exactly like the `[[resources]]` blocks in
`platformr.toml`, so moving a request between them is cut and paste:

```toml
# platformr/requests/eks.toml
[[resources]]
name     = "eks"
pr_title = "feat(eks): add {{.name}}"

  [[resources.fields]]
  name   = "account"
  type   = "select"
  source = "dirs:cloud/aws"
```

How it works:

- **Optional.** No `platformr/requests/` folder means everything comes from
  `platformr.toml`, as before. Requests can also live in both places while moving
  them over.
- **Shared settings apply everywhere.** `[defaults]` (including reusable
  `[defaults.fields]`) and `[maps]` in `platformr.toml` apply to requests in every
  file.
- **A request file can add its own `[maps]`** (e.g. sizes only its requests use).
  It can't set `[defaults]`, `requests_dir`, `templates_dir` or `[[categories]]`;
  those belong in `platformr.toml`.
- **Names must be unique** across `platformr.toml` and all request files, for both
  requests and maps.
- **A broken file only hides its own requests.** platformr shows a warning naming
  the file and the problem (for example the line with the TOML error), and every
  other request still works. `platformr doctor` lists the same warnings.
- **Order.** The picker shows `platformr.toml`'s requests first, then each file's
  requests in alphabetical file order.
- **Different folder.** Set `requests_dir` at the top of `platformr.toml`:

  ```toml
  requests_dir = "ops/platformr-requests"

  [defaults]
  ...
  ```

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

**`when`** leaves a file out unless the expression renders `"true"`, the same as
`when` on fields. Use it for optional parts of a request:

```toml
  [[resources.fields]]
  name    = "addons"
  type    = "multiselect"
  label   = "Optional add-ons"
  options = ["datadog", "platform"]

  [[resources.template_files]]
  name        = "datadog-values.yaml"
  target_path = "clusters/{{.cluster}}/datadog/"
  output_name = "values.yaml"
  when        = '{{if contains "datadog" .addons}}true{{end}}'
```

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

### `login_hint` — how to log in to where the outputs live

Optional. When `platformr status` can't read a request's outputs (no login,
expired session, or nothing found where you're logged in), it shows this
hint. Write whatever your team uses to get into the right account:

```toml
[defaults]
login_hint = "cloudctx {{.account}}"
```

```
You're not logged in to AWS (AWS profile pt/dev:admin), or your session has expired.
  To log in: cloudctx pt-well-iq-dev-services, then run status again.
```

- It's filled in from the request's answers like `output_path`, and
  `{{index .maps.<name> .field}}` works too (e.g. to show an account ID).
- Set it once under `[defaults]` in the org config or `platformr.toml`, or
  per request to override that.
- platformr never chooses credentials or a role. This is only text you write.
  Without it, the message says to log in to the account shown in the request's
  Details.

### `instructions` — anything whoever handles this next should know

Free text, shown in its own section of the PR body, right after the requestor's
own notes and before the raw field dump. Deliberately mechanism-agnostic —
platformr doesn't know or care whether "handling this" means running a specific
`terragrunt`/`terraform` command, waiting on a GitOps controller to reconcile,
getting a manual approval somewhere, or nothing at all:

```toml
[[resources]]
name = "vpc"
instructions = '''
Run `terragrunt run-all apply` from this unit's directory — it includes the
nested eip/ and twingate/ units and applies everything in the correct order
automatically.
'''
```

Supports `{{.field}}` interpolation like `output_path`. Absent means no
section is shown — most resources probably don't need one; it's for the cases
where "just merge it" isn't actually the whole story.

### Change requests: editing existing files

A request with `update_file`, a question with `key` or `list`, or any
`[[resources.changes]]` is a **change request**: it changes values inside an
existing file instead of rendering templates into new ones. The step-by-step
guide is [changing-existing-resources.md](changing-existing-resources.md);
four tested examples are in [`examples/changeable/`](https://github.com/devops-chris/platformr/tree/main/examples/changeable/).

```toml
[[resources]]
name         = "service-scale"
category     = "Service"
display_name = "Scale service"
update_file  = 'apps/{{.name}}/values.yaml'

  [[resources.fields]]
  name   = "name"
  type   = "select"
  source = "dirs:apps"

  [[resources.fields]]
  name    = "replicas"
  type    = "select"
  options = ["1", "2", "3", "4", "6"]
  key     = "replicaCount"            # starts on the current value, writes the answer back

  [[resources.changes]]
  action    = "append"
  key       = "env"
  item      = { name = "SCALED_BY", value = "platformr" }
  unique_by = "name"
```

**On the request**

| Setting | What it does |
|---|---|
| `update_file` | The file to change (supports `{{.field}}`). Read from the base branch |
| `format` | `yaml`, `json`, `hcl`, `env` or `marker`, when the extension doesn't say |

**On a question**

| Setting | What it does |
|---|---|
| `key` | Where in the file this answer goes. The question starts on the current value, shown as "(now: …)", and the current value is always offered even if it's not in `options` |
| `list` | Fill a select with the items of a list (or the names in a map) in the file. Add `allow_manual = true` to also offer typing a new value |
| `show` | With `list`, which field to display when the items are objects |
| `update_file`, `format` | Use a different file for this one question |

**`[[resources.changes]]`**

| Setting | What it does |
|---|---|
| `action` | `append`, `remove`, `put`, `delete`, `set` or `delete_file` |
| `key` | Where in the file (lists and maps for append/remove/put/delete) |
| `item` | What `append` / `put` adds: `"{{.cidr}}"` or `{ name = "{{.user}}", role = "admin" }` |
| `match` | What `remove` takes out: `"{{.cidr}}"` or `{ name = "{{.user}}" }` |
| `name` | The map entry for `put` / `delete` |
| `value` | The new value for `set` |
| `unique_by` | Stop an `append` if the item is already there: `"value"` or a field name |
| `if_exists` | With `unique_by`: `"error"` (default) stops the request, `"skip"` leaves the list as it is and carries on |
| `file`, `format` | Use a different file; for `delete_file`, the file to delete |
| `when` | Skip this change unless the expression is `"true"` |

**Creating and changing in one request.** A request that names templates
(`template`, `template_dir` or `template_files`) *and* has changes does both in one
PR: it renders the new files and edits the existing ones. For example, a new
account's folder plus its name appended to the organization's account list.

**Keys** are a dotted path (`"resources.requests.cpu"`, `"module.eks.cluster_version"`),
or a list when a name contains dots or you need to pick a list item:
`["Statement", { Sid = "{{.sid}}" }, "Action"]`, `["users", 0]`.
`"marker:<name>"` points at a line tagged with a `platformr:<name>` comment, in any
file type.

**Formats** come from the extension: `.yaml`/`.yml` → YAML; `.json`, `.tf.json`,
`.tfvars.json` → JSON; `.tf`, `.tfvars`, `.hcl`, `.nomad` → HCL; `.env`,
`.properties`, `.ini` → key=value.

**Checks:** the request's config is checked before any question is asked (missing
file, unknown format, a change missing what its action needs), and `platformr doctor`
reports the same problems for every request. If a platformr PR changing the same file
is still open, the request stops and links to it. If nothing would change, no PR is
opened.

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

### `status_fields` — what identifies a request in `platformr status`

Each row in `platformr status` needs to say *which* one this is, not just what
kind. By default that's the PR title. To show specific answers instead, list
the fields that matter:

```toml
[[resources]]
name          = "account-location"
display_name  = "Add an environment or region to an account"
status_fields = ["account", "where"]
```

```
account-location  #289  Merged  pt-well-iq-dev-services · qa/use1
```

- Values are read from the PR's Details section, so this works for PRs opened
  before you added the setting, too.
- Fields with no answer are skipped. If none have one, or `status_fields` isn't
  set, the PR title shows instead.
- Pick the fields people would use to tell two requests of this kind apart,
  usually the name and where it lives, not derived values.

---

## The request menu: categories and actions

The menu asks about the **group** first (a kind of thing like Kubernetes, or an audience
like Platform team), then whether you're creating something new or changing something
that already exists, then which request:

```
? What do you need?
  > Network         VPC and remote access into it
    Kubernetes      EKS clusters
    Platform team   Accounts and account-wide setup
? Kubernetes: what do you want to do?
  > Create something new
    Change something that already exists
? Kubernetes: change something that already exists
  > Upgrade its Kubernetes version
    Scale its node group
```

The category step is skipped when there's only one category, and the second question
only appears when a category has both kinds of request. The request list is always
shown once an earlier step was asked, even with one request, so you see what you're
starting and can go back. Once a request is picked, its name, menu group and
description are shown above its questions.

From the second step on, the last choice is **← Back**, and **Esc** does the same:
it returns to the previous step that was asked, starting on what was picked there.
Ctrl+C quits.

Three settings drive it, all optional:

```toml
# org config (.platformr/config.toml): the menu order and a one-line hint per category
[[categories]]
name        = "Network"
description = "VPC and remote access into it"

[[categories]]
name        = "Kubernetes"
description = "EKS clusters"

# any request file
[[resources]]
name     = "eks"
category = "Kubernetes"          # which group it's in

[[resources]]
name     = "eks-upgrade"
category = "Kubernetes"
action   = "change"              # changes something that exists; leave out for "create"
```

| Setting | Where | Default |
|---|---|---|
| `category` | each request | **General** (listed last) |
| `action` | each request | `"create"`; the other value is `"change"` |
| `[[categories]]` | the org config | categories appear in the order their requests are defined, with no hint |

A category used by a request but missing from `[[categories]]` still shows,
after the listed ones. A listed category with no requests is hidden.

The menu lives in the org config because it spans every connected repo: one place,
next to the list of repos it draws from. A single-repo setup can put `[[categories]]`
in that repo's `platformr.toml` instead; categories there are added after the org
config's (a name already in the org config keeps the org config's order and hint).
Within each list, requests appear in the order they're defined.

`action` only sorts the menu. What a request actually does still comes from
its settings (templates for new files, `update_file` for changes; see
[Change requests](#change-requests-editing-existing-files)). Nothing deletes:
removing a resource stays a hand-made PR.

`platformr catalog` uses the same order and splits each category into
"Create something new" and "Change something that already exists".

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

No configuration required — this prompt appears on all resource types. It's the
first section in the PR body (right after the heading) — ahead of `instructions`
and the raw field dump — since it's whatever context the requestor thought was
worth flagging, which is usually the most useful thing to read first.

---

### PR branches

Each PR's branch is `platformr/<request>-<target>`, e.g.
`platformr/vpc-cloud-aws-acme-dev-use1-vpc-main`. If that name is already taken,
because a merged request left its branch behind or another request shares the same
target (two environments added to one account), platformr uses `-2`, `-3`, … instead.
Change requests stop instead if an open request is already changing the same file,
since a second change would start from out-of-date values.

---

## Fields

### Field types

| type | behaviour |
|---|---|
| `input` | Free-text input. Supports `default`, `placeholder`, `validate`, and `optional`. |
| `select` | Dropdown. Populated from `options` (static) or `source` (dynamic). Supports `optional`, `allow_manual`. |
| `multiselect` | Checklist: pick any number. Same option sources as `select` (`options`, `source`, `list`). `default` names the options ticked at the start (comma-separated). The answer is the ticked options joined with commas, e.g. `DevOps,Engineering`. |
| `computed` | No prompt — derives its value from a Go template expression. See [Computed fields](#computed-fields). |
| `file_lookup` | No prompt — fetches a real file from this repo and extracts a value via regex. See [file_lookup fields](#file_lookup-fields-derive-a-value-from-a-real-file-not-a-duplicate). |
| `reviewer` / `team_reviewer` | Like `select`, but the chosen value is also added to the PR's reviewers. See [Selectable reviewers](#selectable-reviewers-developer-chosen). |

### Optional fields

Mark a field `optional = true` to allow it to be left blank. Input fields show
an `(optional)` label; select fields show a `— skip —` option at the top.
Use `{{if .field}}...{{end}}` in templates to omit blocks when the field is empty.

When "none of these" has a real meaning, say it with **`skip_label`** instead of
`— skip —`. It can use earlier answers. Choosing it still gives an empty answer:

```toml
# In an account with environment folders, "none" means the account's own environment
[[resources.fields]]
name          = "env_folder"
type          = "select"
label         = "Environment"
source        = "dirs:cloud/aws/{{.account}}"
optional      = true
skip_label    = "{{if contains \"-prod-\" .account}}prod{{else}}this account's own{{end}} (no folder)"
```

Example:

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

If `default` is set, the input is pre-filled with that value. On a select, the list
starts on it. Both `default` and `placeholder` can use earlier answers:
`default = "{{.current_version}}"`.
If only `placeholder` is set (no `default`), the input is pre-filled with the
placeholder — the user can accept it by pressing Enter or type to replace it.

```toml
[[resources.fields]]
name        = "cidr"
type        = "input"
label       = "VPC CIDR block"
placeholder = "10.0.0.0/16"   # pre-filled; press Enter to accept
```

### Multiple choices — `type = "multiselect"`

A checklist for questions with more than one answer, such as which groups get access.
The answer is stored comma-separated, so templates loop over it with `split`:

```toml
[[resources.fields]]
name        = "groups"
type        = "multiselect"
label       = "Which groups get access?"
list        = "locals.twingate_groups"            # or options = [...] / source = "dirs:..."
update_file = "cloud/aws/_common/twingate-groups.hcl"
default     = "DevOps,Engineering"                # ticked to start with
```

```hcl
# in a template
group_names = [{{range $i, $g := split "," .groups}}{{if $i}}, {{end}}"{{$g}}"{{end}}]
```

At least one must be ticked unless `optional = true`. With `skip_if_empty = true` it
isn't asked when there's nothing to list.

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

The same works at the end of names, and specific entries can be left out:

```toml
[[resources.fields]]
name          = "group"
type          = "select"
source        = "files:cloud/aws/pt-management/global/organization"
filter_suffix = "-accounts"               # only *-accounts files
strip_suffix  = "-accounts"               # "ortho-tops-accounts" is shown as "ortho-tops"
exclude       = ["pt-corp", "pt-security"] # never offer these (compared after stripping)
```

A dynamic select that only applies to some cases can skip itself: with
`skip_if_empty = true`, it isn't asked at all when there's nothing to list, and its
value is `""`. For example, an "Environment" question that lists environment folders,
which most accounts don't have:

```toml
[[resources.fields]]
name          = "env_folder"
type          = "select"
label         = "Environment"
source        = "dirs:cloud/aws/{{.account}}"
exclude       = ["global", "bootstrap", "use1", "usw2"]
skip_if_empty = true
```

### Dynamic select — `source = "dirs:<path>"`

Lists longer than 10 options show 10 at a time and scroll, with a "Type / to search"
hint: typing after `/` narrows the list. This keeps the request heading and earlier
answers on screen.

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
instance** instead of one directory per instance. Lists file
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

### `allow_manual` — let the developer type a value instead of picking one

Add `allow_manual = true` to any `dirs:`/`files:`-sourced select field to offer
a way to type a value instead of only picking from what's already there — useful
when a real value might exist under an older naming convention, or was created
by hand, or just doesn't exist yet:

```toml
[[resources.fields]]
name        = "vpc_name"
type        = "select"
label       = "VPC this cluster lives in"
source      = "dirs:cloud/aws/{{.account}}/{{.region}}/vpc"
allow_manual = true
```

- If the list has existing options, `[+ enter manually]` is added at the
  bottom. Picking it re-prompts as a plain text box.
- If the list is empty, there's nothing to pick from — platformr skips the
  select entirely and goes straight to that same text box.

**Important: typing a value does not create anything, and platformr never
checks it against anything real.** It's a plain string that gets substituted
into the template, full stop. Say `vpc_name` above is for an EKS cluster, and
no VPC exists yet in that account — typing `new-vpc-1` doesn't create a VPC
named `new-vpc-1`. It just makes the EKS template's `dependency` block point
at `vpc/new-vpc-1`, a directory that doesn't exist. That VPC still has to be
requested separately (`platformr request vpc`, naming it `new-vpc-1`) and
merged + applied before the EKS unit will actually plan.

So `allow_manual` doesn't solve "create this thing first" — it just doesn't
*block* finishing the current request while that happens as its own separate
step. If a field's value must always be something real and already applied,
leave `allow_manual` off so the developer can only pick from what's there.

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

### `file_lookup` fields — derive a value from a real file, not a duplicate

`computed` fields are great when the value is a function of answers the developer
already gave. Sometimes the value you actually want lives in a *file* elsewhere in
the repo — an account's vertical/brand, a cluster's real environment name, anything
already declared as a `locals` value in some `.hcl` file that this repo's own
Terragrunt units read directly. Hand-maintaining a second copy of that value inside
`platformr.toml` (a lookup table, a map) works, but it's a duplicate that can drift
from the real file whenever someone updates one and not the other.

`type = "file_lookup"` fetches the real file and extracts the value with a regex —
no prompt, no duplicated data, and it can never go stale since it's reading the
actual source of truth:

```toml
[[resources.fields]]
name    = "vertical"
type    = "file_lookup"
source  = "cloud/aws/{{.account}}/account.hcl"   # supports {{.field}}, same as any path
pattern = '(?m)^\s*vertical\s*=\s*"([^"]*)"'      # exactly one capture group
```

`source` is fetched from this repo (at the same ref templates are read from) and
rendered against already-collected field values first, so it must appear **after**
whatever fields it references — same ordering rule as `computed`. `pattern` needs
exactly one capture group; its match becomes the field's value, available afterward
as `{{.vertical}}` anywhere a field value normally works: `output_path`, `pr_title`,
`when` conditions, and inside `.tmpl` files.

A fetch failure, a pattern with no match, or a pattern with no capture group are all
**hard errors** that stop the request — `file_lookup` is usually feeding something
downstream (a path, a template) where a silently empty or wrong value would be worse
than the request just failing loudly.

Two options for files that aren't always there, or not always in the same place:

```toml
# brand is only set in some accounts: empty instead of an error when it's missing
[[resources.fields]]
name     = "brand"
type     = "file_lookup"
source   = "cloud/aws/{{.account}}/account.hcl"
pattern  = '(?m)^\s*brand\s*=\s*"([^"]*)"'
optional = true

# env.hcl sits at different depths in different accounts: look in this folder, then
# each folder above it, like Terragrunt's find_in_parent_folders
[[resources.fields]]
name           = "environment"
type           = "file_lookup"
source         = "cloud/aws/{{.account}}/{{.where}}/env.hcl"
pattern        = '(?m)^\s*environment\s*=\s*"([^"]*)"'
search_parents = true

# not_found: what to tell the person when the file or value isn't there, instead of
# the technical "couldn't find <path>" error
[[resources.fields]]
name      = "aws_account_id"
type      = "file_lookup"
source    = "clusters/{{.cluster}}/argocd/service-account.yaml"
pattern   = 'arn:aws:iam::(\d+):'
not_found = "This cluster isn't set up for platform projects yet. Ask the platform team."
```

---

### `repo` — read from another repo (the repos are your catalog)

platformr has no database. Your repos are the catalog: an account is a folder, its ID
is in a file, a cluster is a folder. When a request in one repo needs facts that live in
another, such as a Kubernetes GitOps repo needing an account ID from the Terraform repo,
point the question at that repo with `repo`. It works on `dirs:` and `files:` lists and
on `file_lookup`:

```toml
# In the GitOps repo: pick a real account from the Terraform repo, and read its ID there
[[resources.fields]]
name   = "account"
type   = "select"
label  = "AWS account"
source = "dirs:cloud/aws"
repo   = "pt-devops-terraform"

[[resources.fields]]
name    = "account_id"
type    = "file_lookup"
source  = "cloud/aws/{{.account}}/account.hcl"
pattern = 'aws_account_id\s*=\s*"(\d+)"'
repo    = "pt-devops-terraform"
```

- `repo` is the repo's name (or `org/name`). It must be one of the org config's
  `[[repos]]`; otherwise the request stops and says so. A repo that's only read from
  doesn't need a `platformr.toml` of its own (`platformr doctor` notes it).
- It's read at the same branch platformr loaded that repo's config from: its `ref` in the
  org config, or `--ref`.
- Reading only. A request still opens its PR in its own repo.

---

### Reusable fields — `[defaults.fields]`

Some fields are identical across every resource — `account`, or a `file_lookup` like
`vertical` above that every resource in the repo would want the same way. Repeating
the exact same `[[resources.fields]]` block in every resource is real duplication,
and it's easy to let a copy drift once you're editing five of them.

Define the field **once**, under `[defaults]` (repo-level, or org-level in
`.platformr/config.toml` if it should apply across every connected repo), and any
resource opts in by declaring a field with the **same name and no `type` of its
own** — platformr fills in the rest of the definition from the default:

```toml
[defaults]
  [[defaults.fields]]
  name    = "vertical"
  type    = "file_lookup"
  source  = "cloud/aws/{{.account}}/account.hcl"
  pattern = '(?m)^\s*vertical\s*=\s*"([^"]*)"'

[[resources]]
name = "eks"

  [[resources.fields]]
  name   = "account"
  type   = "select"
  source = "dirs:cloud/aws"

  [[resources.fields]]
  name = "vertical"   # bare reference — pulls type/source/pattern from defaults above
```

A few things worth being deliberate about:

- **This isn't limited to `file_lookup`.** Any field type can live in
  `[defaults.fields]` — a `select` sourced from `dirs:`, a `computed` field,
  anything. The reuse mechanism is the same regardless of what kind of field it is.
- **A resource controls where in its own list the reference sits.** Since `vertical`
  needs `{{.account}}` already known, it has to come after `account` in the
  resource's own `fields` — same ordering discipline as everywhere else. Nothing
  forces a default field to run before or after a resource's own fields; the
  resource decides by where it places the bare reference.
- **Opting in is required, not automatic.** A resource that never mentions
  `vertical` is completely unaffected — this is a library to draw from, not
  something injected into every resource whether it's wanted or not.
- **A resource can still fully override it.** Give the field a `type` of its own
  and platformr uses that instead of the default — the name match only fires when
  the resource's own field is bare (no `type` set).

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
validate = "unique"
```

For a request that creates a folder per instance (`template_dir`), this checks that
the rendered `target_path` folder doesn't exist yet. For single-file requests, it
checks `target_path` + `file_name` + `file_ext` (default: `<first field>.yaml`).

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
following helper functions. They work anywhere `{{...}}` does: `.tmpl` files,
`computed` values, `when`, `target_path`, `pr_title`, `instructions` and `output_path`.

The value being changed always goes **last**, so each function also works at the
end of a pipe: `{{replace "-" "_" .name}}` and `{{.name | replace "-" "_"}}` are
the same thing.

| Function | Signature | Example |
|---|---|---|
| `split` | `split sep str` | split comma-separated team IDs into a YAML list |
| `trimPrefix` | `trimPrefix prefix str` | strip a known prefix from a value |
| `trimSuffix` | `trimSuffix suffix str` | strip a known suffix from a value |
| `trimSpace` | `trimSpace str` | drop leading/trailing whitespace from typed input |
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
├── platformr.toml                        ← shared settings (and/or requests)
├── platformr/
│   ├── requests/                         ← optional: one file of requests per thing
│   │   ├── vpc.toml
│   │   └── service.toml
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
