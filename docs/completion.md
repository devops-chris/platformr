# Shell Completion

platformr supports tab completion for subcommands, flags, and resource names
in `request` and `catalog`. Resource names are fetched live from your connected
org, so you need to be connected before completion works.

---

## Setup

### zsh (macOS / Homebrew)

```sh
platformr completion zsh > $(brew --prefix)/share/zsh/site-functions/_platformr
```

Restart your shell. That's it.

For an enterprise install under a custom name (e.g. `pt-platform`):

```sh
pt-platform completion zsh > $(brew --prefix)/share/zsh/site-functions/_pt-platform
```

### zsh (Linux)

```sh
platformr completion zsh > "${fpath[1]}/_platformr"
```

If completion isn't enabled yet, add this to `~/.zshrc` first:

```sh
echo "autoload -U compinit; compinit" >> ~/.zshrc
```

### bash

```sh
platformr completion bash > /etc/bash_completion.d/platformr
```

Or for a per-user install:

```sh
platformr completion bash >> ~/.bash_completion
```

### fish

```sh
platformr completion fish > ~/.config/fish/completions/platformr.fish
```

### PowerShell

```powershell
platformr completion powershell >> $PROFILE
```

---

## What gets completed

| Command | Completions |
|---------|-------------|
| `platformr <TAB>` | subcommands: `request`, `catalog`, `connect`, `auth`, `doctor`, `version` |
| `platformr request <TAB>` | resource names from your connected org, with descriptions |
| `platformr catalog <TAB>` | resource names from your connected org, with descriptions |
| `platformr <cmd> --<TAB>` | available flags for that command |

Resource name completions require a connected org and a valid read token. If
you see no completions for `request` or `catalog`, run `platformr connect <org>`
first.

---

## Testing completion without restarting your shell

```sh
source <(platformr completion zsh)
```

This loads completions for the current session only.
