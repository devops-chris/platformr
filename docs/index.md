# platformr

**Self-service infrastructure requests that arrive as pull requests.**

A developer answers a few questions in the terminal. platformr turns the answers
into files in your infrastructure repo, or changes values in files already there,
and opens a pull request. Your platform team reviews it and applies it the way they
handle any other change.

```bash
brew install devops-chris/tap/platformr
platformr connect my-org
platformr request
```

## What it does

- **Creates new things** from templates your platform team writes: a VPC, a
  cluster, a database, a whole new account.
- **Changes existing things** (day-2): scale a service, upgrade a cluster, allow an
  IP, add a permission. Only the values asked about change; comments and formatting
  stay. See [Changing existing resources](changing-existing-resources.md).
- **Opens one PR** with clear instructions for whoever applies it.

It **doesn't** apply anything or hold cloud credentials, and it doesn't need to know
your IaC tool. Terraform, OpenTofu, Terragrunt, Helm, Crossplane, Pulumi config,
plain YAML and JSON all work the same way.

## Why it helps

- **Developers** don't need to know the repo layout, the IaC tool, or naming
  conventions. They pick from lists, and lists can come from the repo itself.
- **Platform teams** get every request as a consistent, reviewable PR that already
  follows their conventions.
- **Nothing new to run or trust.** No server, no cloud credentials, no state. It
  uses GitHub and the PR process you already have.

## What the platform team sets up

| Piece | Lives in | What it is |
|---|---|---|
| `platformr.toml` | each IaC repo (root) | Shared settings, and/or the request types and their questions |
| `platformr/requests/*.toml` | each IaC repo (optional) | Requests split into one file per thing |
| `platformr/templates/` | each IaC repo | Files a create request generates, with `{{.answer}}` blanks |
| `.platformr/config.toml` | one org-level repo | Which IaC repos platformr reads |

Developers only ever run `platformr request`.

## Next

- [Changing existing resources](changing-existing-resources.md): day-2 requests,
  with four [worked examples](https://github.com/devops-chris/platformr/tree/main/examples/changeable).
- [Configuration reference](configuration.md): every setting.
- [README on GitHub](https://github.com/devops-chris/platformr#readme): install,
  org setup and GitHub App.
