# platformr

A configurable developer self-service CLI. Developers request infrastructure
and services interactively — platformr opens a pull request against your GitOps
repos on their behalf. No cloud credentials, no IaC tooling, no context switching.

```
brew install devops-chris/tap/platformr
platformr connect my-org
platformr request
```

---

## How it works

1. Developer runs `platformr request` and picks a resource type (service, database, VPC, etc.)
2. CLI prompts for the required fields interactively
3. Opens a PR against the appropriate IaC repo with a rendered template
5. Your existing CI/CD (ArgoCD, Flux, Terraform Cloud) applies it as normal

Resource types, templates, and target repos are entirely defined by your platform
team — nothing is hardcoded in the binary.

---

## Developer quick start

```bash
# Install
brew install devops-chris/tap/platformr

# Connect to your org (one time)
platformr connect my-org

# Authorize PR creation (one time — browser prompt, ~10 seconds)
platformr auth

# Make a request
platformr request

# See what's available
platformr catalog
platformr catalog service        # field-level schema
platformr catalog service --json # machine-readable
```

---

## Platform admin guide

This section is for the team adopting platformr for their org.

### 1. Create the org config repo

Create a **private or internal** repo named `.platformr` in your GitHub org.
Add a `config.toml`:

```toml
[github]
default_org   = "my-org"
app_client_id = ""        # fill in after step 3

[defaults]
base_branch = "main"

[[repos]]
url = "platform-claims"   # shorthand — resolves to my-org/platform-claims

[[repos]]
url = "terraform-infra"

[[repos]]
url = "other-org/shared-infra"  # cross-org repos work too

# Optional: fetch platformr.toml and templates from a specific branch.
# PRs still target base_branch — this only controls where config is read from.
# [[repos]]
# url = "terraform-infra"
# ref = "my-test-branch"
```

> **Visibility:** Use **Internal** on GitHub Enterprise so all org members can
> read it automatically. Private requires explicit access grants.

### 2. Add `platformr.toml` to each IaC repo

In each repo listed above, add a `platformr.toml` to the repo root.
Templates live alongside it in `platformr/templates/`.

See [`examples/iac-repo/platformr.toml`](examples/iac-repo/platformr.toml) for
a full annotated example with Crossplane and Terraform resources.

### 3. Set up write access for PR creation

Developers need read access to IaC repos (handled by Internal visibility) but
also write access to create branches and PRs. There are two options:

#### Option A — GitHub App (recommended)

The cleanest approach. Developers authorize the app once via browser; the app
creates PRs on their behalf. Developers never need write access to IaC repos directly.

**Create the app:**

The fastest way is the manifest flow — GitHub pre-fills all settings from a
JSON file:

1. Go to: `https://github.com/organizations/MY-ORG/settings/apps/new?state=platformr`
2. Paste the contents of [`github-app-manifest.json`](github-app-manifest.json)
   into the manifest field, or use the GitHub API:

```bash
# Create app from manifest via API
gh api /orgs/MY-ORG/apps \
  --method POST \
  --field manifest=@github-app-manifest.json
```

3. Install the app on your IaC repos (not org-wide — only the repos platformr targets)
4. Copy the **Client ID** (public, not a secret) into `.platformr/config.toml`:

```toml
[github]
app_client_id = "Iv1.xxxxxxxxxxxx"
```

Developers then run `platformr auth` once. Their token is stored in the OS
keychain (macOS Keychain, Linux Secret Service, Windows Credential Manager) —
never as plaintext on disk.

#### Option B — Team write access + branch protection

Simpler to set up, works without a GitHub App.

1. Add developers to a GitHub team with `write` permission on IaC repos
2. Enable branch protection on `main`: require PRs, no direct pushes
3. Skip `platformr auth` — developers use their personal `gh` CLI token

Trade-off: developers have write access beyond just platformr. Branch protection
mitigates direct push risk but does not restrict what they can push to branches.

#### Option C — Fine-grained PAT

Create a fine-grained personal access token scoped to PR creation on specific
repos. Distribute it via your secrets manager (e.g. `lockr get PLATFORMR_TOKEN`).
Developers set `PLATFORMR_TOKEN` in their environment.

Good for teams that can't use a GitHub App and want tighter scope than Option B.

### 4. Distribute to developers

Create a private brew tap with a formula that connects to your org automatically:

```ruby
class Platformr < Formula
  desc "Developer self-service platform CLI"
  url "https://github.com/devops-chris/platformr/releases/download/vX.Y.Z/platformr_darwin_arm64.tar.gz"

  def install
    bin.install "platformr"
  end

  def post_install
    system "#{bin}/platformr", "connect", "my-org"
  end
end
```

Developers run one command and are connected:

```bash
brew install my-org/tap/platformr
platformr auth   # only needed if using Option A (GitHub App)
platformr request
```

---

## Configuration reference

See [`docs/configuration.md`](docs/configuration.md) for the full schema of:
- `.platformr/config.toml` (org config)
- `platformr.toml` (per IaC repo)
- Field types, validation, dynamic selects, template syntax

---

## Auth token resolution

platformr uses separate tokens for read and write operations so the GitHub App
only needs access to repos it creates PRs in — not the config/template repos.

| Source | Used for |
|---|---|
| OS keychain (`platformr auth`) | PR creation — attributed to the GitHub App |
| `GITHUB_TOKEN` / `GH_TOKEN` env var | Read + write fallback |
| `gh auth token` | Read + write fallback |

Read operations (fetching config, templates, directory listings) always use the
env var / `gh` CLI token. `platformr auth` is only needed if you want PRs
attributed to the app rather than your personal account.

For **GitHub Enterprise Server**, set `GH_HOST=github.mycompany.com` before
connecting. platformr mirrors the `gh` CLI convention.

---

## Commands

| Command | Description |
|---|---|
| `platformr connect <org>` | Connect to an org's `.platformr` config |
| `platformr auth` | Authorize PR creation via GitHub App device flow |
| `platformr auth logout` | Remove stored authorization token |
| `platformr request` | Interactively request a new resource |
| `platformr catalog` | List available resource types |
| `platformr catalog <name>` | Show field schema for a resource |
| `platformr catalog --json` | Machine-readable schema output |
| `platformr version` | Print version info |

---

## License

MIT — see [LICENSE](LICENSE)
