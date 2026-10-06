# Changing existing resources

> **Status: planned, not built yet.** This describes how platformr will support
> requests that *change* or *remove* something it created: upgrade a cluster, scale
> a service, remove a user. The repo layouts below work today and are worth adopting
> now. Settings marked *proposed* don't exist yet and may change.
>
> To see it configured end to end, start with the two side-by-side examples in
> [`examples/changeable/`](../examples/changeable/).

---

## Who knows what

**platformr never understands HCL, YAML, JSON or any IaC tool.** It asks questions,
fills in blanks in text files, writes them, and opens a PR. Everything specific to
your infrastructure lives in your repo's config and templates, written by your
platform team:

| Part | Where it lives | Who changes it |
|---|---|---|
| Which requests exist ("EKS › Upgrade cluster version") | `platformr.toml` | platform team |
| The questions, the allowed choices, the labels | `platformr.toml` | platform team |
| The file format, and where each answer goes in it | templates | platform team |
| Where files are written | `platformr.toml` | platform team |
| **How** to ask, fill blanks, write files, open a PR | platformr | platformr release |

A template is just the real file with blanks:

```hcl
# platformr/templates/eks/instance.auto.tfvars.tmpl
cluster_version = "{{.cluster_version}}"
```

platformr replaces `{{.cluster_version}}` with the answer to the question named
`cluster_version` and writes the text. It doesn't know this is Terraform. The same
works for a Crossplane claim, a Helm values file, or a Pulumi config file.

So when something new comes up:

| New scenario | Config, or platformr update? |
|---|---|
| A new IaC tool (Pulumi, Bicep, a new Crossplane CRD) | config + templates |
| A new action ("Upgrade version", "Add user") | config + templates |
| A new size, an approved version, a new brand | config |
| **Changing** a file platformr already wrote | **platformr, once**; then every change action is config |
| **Removing** a file platformr wrote | **platformr, once**; then config |

platformr only needs an update for a new kind of *mechanic*, and there are three:
**create**, **change**, **remove**. Each is generic and built once.

---

## The one rule

**For each thing it manages, platformr owns a whole file, and nobody edits that
file by hand.**

A change request re-creates the owned file from the answers, and the PR shows the
difference. Everything people want to hand-edit goes in a *different* file, which the
owned file sits next to.

| Tool | The file platformr owns | How the tool picks it up |
|---|---|---|
| Terraform | `instance.auto.tfvars` | Terraform loads `*.auto.tfvars` automatically |
| Terragrunt | `instance.hcl` | `terragrunt.hcl` reads it with `read_terragrunt_config` |
| Helm (Argo CD) | `values-platformr.yaml` | listed **last** in `valueFiles`, so it wins |
| Crossplane | the claim YAML | it *is* the instance; Crossplane reads it in the cluster |
| Kustomize | a patch file per instance | listed in the overlay's `patches` |
| Pulumi / CDK / CDKTF | `instances/<name>.yaml` | the code loops over `instances/*.yaml` |

### What doesn't fit

- **Values inside a file shared by many instances**: one big `values.yaml` for every
  service, a Terraform list of every account. platformr can't change one entry
  without rewriting everyone else's.
- **Instances defined in code**: `new EksCluster(this, "apps", {version: "1.32"})`.
  platformr writes data, never code.

The fix is the same for both: one data file per instance, and have the IaC read
them. For example, one `.tf` file per account (Terraform reads every `.tf` in a
folder), or a Pulumi program that loops over `instances/*.yaml`.

---

## Requests are actions

Instead of one big "change EKS" request, each action is its own small request,
grouped in the picker with `category`:

```
? What type of resource?          › EKS
? What would you like to request? ›  New cluster
                                     Upgrade cluster version
                                     Add user
                                     Remove user
```

Each action asks **only** what it changes. Everything it doesn't ask keeps its
current value. Actions come in three kinds:

| Action | Kind | What platformr does | Built? |
|---|---|---|---|
| New cluster | create | writes new files | yes |
| Upgrade cluster version | change | rewrites the owned file, asking only for the version | planned |
| Add user | create | writes **one small file per user**: `clusters/apps/users/jane.yaml` | yes |
| Remove user | remove | deletes that user's file | planned |

**Rule of thumb:**
- **A single setting** (version, size, count) lives in the owned file and changes
  with a **change** action.
- **A collection** (users, DNS records, allowed IPs) uses **one file per item**, added
  with **create** and taken away with **remove**. The IaC reads the whole folder:
  - Terraform: `fileset()`
  - Crossplane: one claim per item
  - Pulumi/CDK: a loop

  That keeps platformr from ever needing to edit a list.

---

## How a change request works

1. **Pick what to change.** The developer picks from things that exist, for example
   the folders under `clusters/`.
2. **Load its current answers.** Every platformr PR records its answers and the
   template version it used, in a hidden block in the PR description. platformr
   reads them from the last merged platformr PR for that resource. Nothing extra is
   stored in the repo.
3. **Check nobody edited it by hand.** platformr fills the template (at that version)
   with those answers and compares the text to the file on the main branch. If they
   differ, someone changed it outside platformr: the request stops and says to ask
   the platform team. This is a plain text comparison and works for any format.
4. **Ask only this action's questions**, showing the current value.
5. **Show the change** (`cluster_version 1.32 → 1.33`) and ask to confirm.
6. **Open the PR.** The owned file is re-filled with the same template version, the
   kept answers, and the new ones. The diff shows only what the developer changed,
   and the PR description repeats the old → new summary.

Two guard rails:

- **One open change at a time.** If a platformr PR for that resource is already open,
  the request stops right after step 1 and links to it.
- **Only resources created by platformr can be changed**, since step 2 needs recorded
  answers. Bringing in resources created by hand ("adopt") isn't designed yet.

### Approved choices and friendly names

The platform team decides what can be picked. A plain list is enough for versions:

```toml
options = ["1.32", "1.33", "1.34"]   # only approved versions
```

For sizes, a named list *(proposed)* shows a plain word next to the real value:

```toml
[maps.node_sizes]
small  = "m6i.large"
medium = "m6i.2xlarge"

[[resources.fields]]
name        = "node_instance_type"
type        = "select"
label       = "Node size"
options_map = "node_sizes"       # proposed: shows "medium (m6i.2xlarge)"
```

The real value is what gets written, so the IaC never needs to know what "medium"
means. If a resource's current value is no longer approved (a cluster still on
1.31), it stays pickable as `1.31 (current)` so other things can be changed without
forcing an upgrade. It can't be picked for new resources.

---

## Notes by tool

**Crossplane.** platformr only writes the claim. It never reads the XRD or the
Composition and doesn't know Crossplane exists. The platform team writes a claim
template that matches their XRD, and Crossplane does the rest in the cluster.
Settings people tune by hand belong in the Composition, since the claim is owned by
platformr.

**Helm.** See the pitfalls below; most are about how Helm merges values files.

**Pulumi.** Don't let platformr own `Pulumi.<stack>.yaml`. Pulumi writes to it too
(`pulumi config set`) and it holds encrypted secrets, which a rewrite would wipe.
Use a separate `instances/<name>.yaml` that the program reads.

**CDK, CDKTF, Bicep.** Same pattern: a per-instance JSON/YAML (or Bicep parameter)
file that the code or deployment reads.

---

## What can go wrong, and the fix

| Problem | Happens when | Fix |
|---|---|---|
| Nothing for platformr to own | Values sit in a shared file, inline in a manifest (Argo CD `helm.values`), or in code | Move this instance's values to their own file |
| Helm can't find the values file | The chart comes from a Helm repo, not git | Use a multi-source Argo CD Application (`$values` ref, Argo CD 2.6+) |
| A list from `values.yaml` disappears | The platformr file sets a list (`env`, `tolerations`) | Helm replaces lists instead of merging them. Keep owned files to simple values: counts, sizes, versions, flags |
| A change seems to do nothing | Another values file comes after the platformr one | List the platformr file last |
| Argo CD errors on a missing file | The Application lists the platformr file before it exists | Create it in the same request that creates the app, or set `ignoreMissingValueFiles: true` |
| Flux `HelmRelease` can't read a values file from git | Flux takes values inline or from a ConfigMap | Generate the ConfigMap from the file with Kustomize's `configMapGenerator` |
| Argo CD tries to apply `values.yaml` as a manifest | An app-of-apps syncs the folder holding each service's `application.yaml` and values | Limit it with `directory.include: "*/application.yaml"` |
| Pulumi secrets disappear | platformr owns `Pulumi.<stack>.yaml` | Own a separate `instances/<name>.yaml` instead |
| Request stops: "edited by hand" | Someone changed the owned file directly | Move the hand-written part to a file people own, or ask the platform team to re-align it |

---

## Proposed `platformr.toml` settings

*None of these exist yet; names may change.*

| Setting | Where | What it does |
|---|---|---|
| `mode = "update"` | resource | A change action: rewrites the owned file. Stops if it doesn't exist or a change is already open |
| `mode = "remove"` | resource | A remove action: deletes the owned file(s) it points at |
| `options_map = "<map>"` | select field | Show friendly names from a `[maps]` table, write the real value |
| `{{.field}}` in `default` | input/select field | Pre-fill from an earlier answer, e.g. a `file_lookup`. Only for resources without recorded answers |

A change action points at just the owned template with today's single-file
settings (`template`, `target_path`, `file_name`, `file_ext`), so it never touches the
files people own. See [`examples/changeable/`](../examples/changeable/).
