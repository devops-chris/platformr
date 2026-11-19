# Guides: several requests in order

Some things take several requests, one after the other: an account, then its network, then a
cluster. Each needs the one before it **merged**, because platformr reads the repo's base
branch, not your branch and not what's been applied. A **guide** puts those requests in
order and shows where you are:

```
  Set up an environment  Start here · guide
  pt-ortho-mt-prod-services · use1
  Network is waiting on https://github.com/org/repo/pull/361

? Pick a step to start it
  ✓ 1. Account          done
  … 2. Network          waiting: PR #361 isn't merged yet
  ○ 3. Cluster          needs the network merged
  ○ 4. Database         optional · only if the team needs one
    Done for now        run this again any time to pick up where you left off
```

Picking a step starts its request with the guide's answers already filled in (shown, not
asked again). When that PR is open, the guide comes back to the checklist and shows the step
as waiting. Run the guide again after merging and the step shows as done.

## How a step's state is worked out

Every time the checklist is shown:

- **done**: the step's `done_when` path is on the base branch of the repo its request opens
  PRs in.
- **waiting**: otherwise, there's an open platformr PR from the step's request whose Details
  have the guide's answers (every answer they share is the same, e.g. the same account and
  environment). A PR opened from the guide in this run counts straight away.
- **to do**: neither. The first required step to do is marked **next** and starts selected.

Starting a step before an earlier required one is done asks first. A step waiting on its PR
can't be started again from the guide (merge it, or run the request on its own).

## Config

A guide is a request with `[[resources.steps]]`. Its questions work like any request's
(including reusable fields), and are asked once:

```toml
[[resources]]
name         = "environment"
category     = "Start here"
display_name = "Set up an environment"
description  = "Shows what an account's environment has, what's waiting on a PR, and what's next"

  [[resources.fields]]
  name = "account"            # reusable fields from [defaults.fields]
  [[resources.fields]]
  name = "env_folder"
  [[resources.fields]]
  name = "region"
  [[resources.fields]]
  name = "where"

  [[resources.steps]]
  title     = "Network"
  request   = "vpc"
  done_when = "dirs:cloud/aws/{{.account}}/{{.where}}/vpc"
  hint      = "VPC and its Twingate connector"

  [[resources.steps]]
  title     = "Cluster"
  request   = "eks"
  done_when = "dirs:cloud/aws/{{.account}}/{{.where}}/eks"

  [[resources.steps]]
  title     = "Database"
  request   = "rds"
  optional  = true
  done_when = "dirs:cloud/aws/{{.account}}/{{.where}}/rds"

  [[resources.steps]]
  title     = "Cluster apps"
  request   = "cluster-addons"        # a request in another connected repo
  done_when = "dirs:cloud/aws/{{.account}}/use1"
```

| Setting | What it does |
|---|---|
| `title` | The step's name in the checklist. Defaults to the request's display name |
| `request` | The request the step runs, from any connected repo. It can't be another guide |
| `done_when` | A path that's there once the step is done, checked in the step request's repo on its base branch: a file or folder, or `dirs:<path>` for "a folder with at least one folder in it" (e.g. any VPC). Leave it out for a step that's never shown as done |
| `optional` | Offered, but never the next step and never blocks later ones |
| `when` | Leave the step out unless it renders `"true"`, like a field's `when`, e.g. a step only for non-prod accounts |
| `hint` | A short line next to the step, shown until it's done |

All of them can use the guide's answers (`{{.account}}`).

**Filling in answers.** A step's request doesn't ask any question the guide already
answered, matched by name (an empty answer, such as "no environment folder", counts too).
Computed fields and `file_lookup` fields are still worked out by the request itself, and
change-request questions that read a file (`key`, `list`) are always asked. So keep the
guide's question names the same as the requests' (`account`, `env_folder`, `region`).

**Finding open PRs.** platformr searches the platformr PRs in each step's repo for the
guide's first answer (e.g. the account name), then compares the PR's Details.

**Mistakes.** A step naming a request that doesn't exist, or another guide, stops the guide
with a message saying so, before anything is shown.
