# Checking on requests

Every request opens a pull request. `platformr status` finds those PRs and
tells you where each one stands: **Open** (waiting on review), **Merged**
(accepted), or **Closed** (closed without merging). Pick one to see its details,
its outputs, and its URL.

```
$ platformr status

  Your requests  sam — 1 open, 2 merged

? Find a request
  Press / then type to search — e.g. a resource type, a word from its title, a PR number, or a login.
> s3-bucket  #42  Open             Add S3 bucket orders-exports
  rds        #39  Merged  ✓ outputs Add RDS instance billing-db
  s3-bucket  #31  Merged  ✓ outputs Add S3 bucket ml-artifacts
  — skip —
```

`✓ outputs` marks requests that list output values (`output_path` in the
[configuration reference](configuration.md)). If `lockr` is installed, you can
reveal one of them right there.

---

## Someone else's requests

By default you only see PRs **you** opened. Two flags widen that:

| Command | Shows |
|---|---|
| `platformr status` | Your requests |
| `platformr status --user alex` | Requests opened by the GitHub user `alex` |
| `platformr status --everyone` | Requests opened by anyone |

Use them to check whether a teammate already asked for the same thing before you
open a duplicate. A platform engineer can use them to see what's in flight:

```
$ platformr status --everyone

  All requests — 3 open, 5 merged
  Showing the 20 most recent — add --all for the full history.

? Find a request
> s3-bucket  #42  sam    Open             Add S3 bucket orders-exports
  eks        #41  alex   Open             Upgrade cluster apps to 1.33
  rds        #40  priya  Open             Add RDS instance search-db
  rds        #39  sam    Merged  ✓ outputs Add RDS instance billing-db
  ...
```

When the list mixes people, each row shows who opened it, and the detail view
adds a **By:** line. Press `/` and type a login to narrow the list to one person,
the same way you'd search by resource type or title.

`--user` and `--everyone` can't be used together. Either one combines with
`--all`.

### What you can see

There's no separate permission system. `status` searches GitHub as you, so it
returns exactly the PRs you could already see on github.com: the ones in IaC
repos you have read access to.

Seeing a request doesn't mean you can read its secrets. Revealing an output value
still goes through lockr and **your own** cloud credentials, so a teammate's
database password stays out of reach unless your role can read it anyway.

---

## Limits and history

- By default you get the **20 most recent** open and merged requests.
  Closed-without-merge requests are hidden, since those are abandoned.
- `--all` includes closed requests and removes the 20-request limit.

```
platformr status --all
platformr status --everyone --all
```

## How requests are found

Every PR platformr opens gets a `platformr` label. `status` runs a GitHub search
per registered IaC repo for PRs with that label, plus `author:<login>` unless
you passed `--everyone`. Filtering happens on GitHub's side, so platformr never
downloads a repo's full PR list. Nothing is stored locally: status always comes
live from GitHub, so a PR merged a minute ago already shows as merged.

PRs opened by hand without the `platformr` label don't show up. If you remove
the label from a PR, it stops showing up too.
