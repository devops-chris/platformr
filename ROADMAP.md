# Roadmap

Future enhancements and feature ideas for platformr.

## Planned

### `platformr setup` — Guided org onboarding wizard
An interactive setup command for platform admins (not end users) that walks
through the full org configuration:
- Creates the `.platformr` repo in the org
- Generates `config.toml` interactively
- Walks through GitHub App creation using the manifest flow
- Optionally scaffolds `platformr.toml` and template stubs in an existing IaC repo

Goal: platform team can go from zero to a working org config in one command.

### `platformr request --dry-run`
Runs the full interactive flow but skips PR creation, printing the rendered
template output instead. Useful for testing config and templates without
touching GitHub.

### Remote config caching
Cache fetched `platformr.toml` configs locally with a configurable TTL.
Reduces GitHub API calls on every `request` and `catalog` invocation, and
allows basic offline operation.

### Unit tests
Test coverage for the core logic that doesn't require GitHub:
- Config resolver (defaults merging, `{{.resource}}` interpolation)
- Template rendering
- Field validation

## Ideas

### Shell completions
Tab completion for resource names and field values using Cobra's built-in
completion support.

### `platformr status <name>`
Check the status of a previously requested resource — whether the PR is open,
merged, or if the IaC has been applied.

### PR templates
Support org-level PR body templates so requests include consistent context
(team, ticket reference, runbook link, etc.).

### Multiple connected orgs
Allow `platformr connect` to register multiple orgs, with `platformr use <org>`
to switch between them. Useful for engineers who span multiple GitHub orgs.

### VS Code / IDE extension
Surface `platformr catalog` and `platformr request` directly from the IDE.

## Contributing

Have an idea? Open an issue or discussion on GitHub. Pull requests welcome.

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.
