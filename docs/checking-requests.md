# Checking on requests

Every request opens a pull request. `platformr status` finds those PRs and
tells you where each one stands: **Open** (waiting on review), **Merged**
(accepted), or **Closed** (closed without merging). Pick one to see its details,
its outputs, and its URL.

```
$ platformr status

  Your requests  sam — 1 open, 2 merged

    New S3 bucket                    2  (1 open)
    New Aurora PostgreSQL database   1

? Find a request
  Press / then type to search — e.g. a resource type, a word from its title, a PR number, or a login.
> s3-bucket  #42  Open             orders-exports · prod/use1
  rds        #39  Merged  ✓ outputs billing-db · prod/use1
  s3-bucket  #31  Merged  ✓ outputs ml-artifacts · dev/use1
  — skip —
```

Above the list is a count of each kind of request, using the names from the
request menu. Each row starts with the request type and ends with what
identifies that one request. The platform team picks those values per request
type with [`status_fields`](configuration.md#status_fields-what-identifies-a-request-in-platformr-status).
Request types without it show the PR title.

`✓ outputs` marks requests that list output values (`output_path` in the
[configuration reference](configuration.md)). If `lockr` is installed, you can
reveal one of them right there.

---

## The flags

`status` answers two separate questions, and each has its own flags:

| Question | Default | Change it with |
|---|---|---|
| **Whose** requests? | yours | `-u <login>` for one person, `-e` for everyone |
| **Which** requests? | the 20 most recent open or merged | `-a` for the full history, including closed |

Add **search words** to narrow either one. Everything combines:

```
platformr status                 # your 20 most recent requests
platformr status -a              # all of yours, including closed
platformr status -e              # everyone's 20 most recent
platformr status -e orders       # everyone's requests mentioning "orders"
platformr status -u alex rds     # alex's requests mentioning "rds"
platformr status -e -a orders    # every request ever that mentions "orders"
```

`platformr status --help` shows the same summary.

---

## Someone else's requests

Use `-e` to check whether a teammate already asked for the same thing before
you open a duplicate. A platform engineer can use it to see what's in flight:

```
$ platformr status -e

  All requests — 3 open, 5 merged
  Showing the 20 most recent — add search words to narrow, or -a for the full history.

    New Aurora PostgreSQL database   3  (2 open)
    New S3 bucket                    2  (1 open)
    ...

? Find a request
> s3-bucket  #42  sam    Open             orders-exports · prod/use1
  eks        #41  alex   Open             apps · 1.33
  rds        #40  priya  Open             search-db · dev/use1
  rds        #39  sam    Merged  ✓ outputs billing-db · prod/use1
  ...
```

With `-e`, every row shows who opened it, and the detail view adds a **By:**
line. With `-u alex`, the heading says whose list it is ("alex's requests").
`-u` and `-e` can't be used together.

### What you can see

There's no separate permission system. `status` searches GitHub as you, so it
returns exactly the PRs you could already see on github.com: the ones in IaC
repos you have read access to.

Seeing a request doesn't mean you can read its secrets. Revealing an output value
still goes through lockr and **your own** cloud credentials, so a teammate's
database password stays out of reach unless your role can read it anyway.

---

## When there are a lot of requests

An org can build up hundreds of requests. Three things keep the list usable:

1. **The 20 most recent are shown by default.** The summary line tells you when
   there are more.
2. **Search words are sent to GitHub's search,** which matches them against each
   request's title and details (account, name, region, and so on). Only matching
   requests are downloaded, so `platformr status -e -a orders` stays fast however
   much history exists. Several words must all match; put quotes around an exact
   phrase (`status -e "billing db"`).
3. **Inside the list, press `/`** and type to filter what's already shown, by
   resource type, title, PR number, or login.

So the usual pattern: start broad (`status -e`). If the request you want isn't
in the 20 shown, add a word (`status -e billing`), and add `-a` if it might be old
or closed.

---

## How requests are found

Every PR platformr opens gets a `platformr` label. `status` runs a GitHub search
per registered IaC repo for PRs with that label, plus `author:<login>` unless
you passed `-e`, plus any search words you gave. Filtering happens on GitHub's
side, so platformr never downloads a repo's full PR list. Nothing is stored locally: status always comes
live from GitHub, so a PR merged a minute ago already shows as merged.

PRs opened by hand without the `platformr` label don't show up. If you remove
the label from a PR, it stops showing up too.
