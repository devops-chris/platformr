# Change requests: four examples

A **change request** edits values inside a file that already exists, instead of
creating new files. Each example below has:

| In the folder | What it is |
|---|---|
| `platformr.toml` | the request, exactly as you'd put it in your repo |
| the repo files (`apps/`, `clusters/`, ...) | what's in the repo **before** the request |
| `expected/` | the same file **after** the request: what the PR contains |

The full explanation is in
[docs/changing-existing-resources.md](../../docs/changing-existing-resources.md).
`go test ./cmd -run Examples` runs every example through platformr's real code
and checks the result against `expected/` byte for byte.

| Example | Format | Kind of change |
|---|---|---|
| [Scale a service](#1-helm-scale-a-service) | YAML (Helm values) | change two values |
| [Upgrade an EKS cluster](#2-terraform-upgrade-an-eks-cluster) | HCL (Terraform) | change one value |
| [Allow / remove an IP](#3-security-group-allow-or-remove-an-ip) | HCL (Terraform) | add to / remove from a list |
| [Add a permission to a policy](#4-iam-policy-add-a-permission) | JSON (IAM) | add to a list inside one statement |

All four work the same way: **`update_file`** says which file, **`key`** says where
in it, and **`[[resources.changes]]`** adds or removes items. platformr reads the file
format from the file extension. Nothing else in the file changes: comments, order
and formatting stay as they were.

---

## 1. Helm: scale a service

**`platformr.toml`**
```toml
[[resources]]
name         = "service-scale"
category     = "Service"
display_name = "Scale service"
update_file  = 'apps/{{.name}}/values.yaml'

  [[resources.fields]]
  name   = "name"
  type   = "select"
  label  = "Which service?"
  source = "dirs:apps"

  [[resources.fields]]
  name    = "replicas"
  type    = "select"
  label   = "How many copies?"
  options = ["1", "2", "3", "4", "6"]
  key     = "replicaCount"

  [[resources.fields]]
  name    = "cpu"
  type    = "select"
  label   = "CPU per copy"
  options = ["250m", "500m", "1"]
  key     = "resources.requests.cpu"
```

A question with a **`key`** starts on the value that's in the file now, and the
answer is written back to the same spot. `resources.requests.cpu` reaches into the
nested YAML.

**What the developer sees**
```
? Which service?                  › payments
? How many copies? (now: 2)       › 4
? CPU per copy (now: 500m)        › 1

This request changes:
  How many copies?: 2 → 4
  CPU per copy: 500m → 1
```

**The PR**
```diff
 # payments service — written by hand
 image:
   repository: ghcr.io/acme/payments
   tag: "1.4.0"
-replicaCount: 2  # bump for peak season
+replicaCount: 4  # bump for peak season
 resources:
   requests:
-    cpu: 500m
+    cpu: "1"
     memory: 512Mi
```

`cpu` was text (`500m`), so the new value stays text: `"1"`, not the number `1`.

---

## 2. Terraform: upgrade an EKS cluster

**`platformr.toml`**
```toml
[[resources]]
name         = "eks-upgrade"
category     = "EKS"
display_name = "Upgrade cluster version"
update_file  = 'clusters/{{.name}}/main.tf'

  [[resources.fields]]
  name   = "name"
  type   = "select"
  label  = "Which cluster?"
  source = "dirs:clusters"

  [[resources.fields]]
  name    = "cluster_version"
  type    = "select"
  label   = "Kubernetes version (one step at a time, e.g. 1.32 → 1.33)"
  options = ["1.33", "1.34"]
  key     = "module.eks.cluster_version"
```

In HCL, a key walks through blocks by type and name: `module.eks.cluster_version` is
the `cluster_version` attribute in `module "eks" { }`. `options` is the approved
list. The cluster's current version is always offered as well, even when it's no
longer on the list, so it can be kept as it is.

**The PR**
```diff
   cluster_name    = "apps"
-  cluster_version = "1.32"
+  cluster_version = "1.33"
```

---

## 3. Security group: allow or remove an IP

**`platformr.toml`**
```toml
[[resources]]
name         = "sg-allow-ip"
category     = "Network"
display_name = "Allow an IP"
update_file  = "network/security-groups.tf"

  [[resources.fields]]
  name  = "cidr"
  type  = "input"
  label = "IP range to allow (CIDR, e.g. 203.0.113.10/32)"

  [[resources.changes]]
  action    = "append"
  key       = "resource.aws_security_group_rule.https.cidr_blocks"
  item      = "{{.cidr}}"
  unique_by = "value"

[[resources]]
name         = "sg-remove-ip"
category     = "Network"
display_name = "Remove an IP"
update_file  = "network/security-groups.tf"

  [[resources.fields]]
  name  = "cidr"
  type  = "select"
  label = "Which IP range?"
  list  = "resource.aws_security_group_rule.https.cidr_blocks"

  [[resources.changes]]
  action = "remove"
  key    = "resource.aws_security_group_rule.https.cidr_blocks"
  match  = "{{.cidr}}"
```

- **`[[resources.changes]]`** with `action = "append"` adds an item to a list.
  `unique_by = "value"` stops the request if that CIDR is already there.
- **`list`** on a question fills the picker with what's in the list now, so "Remove
  an IP" only offers IPs that exist.

**The PRs**
```diff
   cidr_blocks = [
     "10.0.0.0/8",     # office VPN
     "198.51.100.0/24",
+    "203.0.113.10/32",
   ]
```
```diff
   cidr_blocks = [
     "10.0.0.0/8",     # office VPN
-    "198.51.100.0/24",
   ]
```

---

## 4. IAM policy: add a permission

**`platformr.toml`**
```toml
[[resources]]
name         = "iam-add-action"
category     = "IAM"
display_name = "Add a permission"
update_file  = 'policies/{{.policy}}.json'

  [[resources.fields]]
  name   = "policy"
  type   = "select"
  label  = "Which policy?"
  source = "files:policies"

  [[resources.fields]]
  name  = "sid"
  type  = "select"
  label = "Which statement?"
  list  = "Statement"
  show  = "Sid"

  [[resources.fields]]
  name    = "action"
  type    = "select"
  label   = "Permission to add"
  options = ["s3:PutObject", "s3:DeleteObject", "s3:GetObjectTagging"]

  [[resources.changes]]
  action    = "append"
  key       = ["Statement", { Sid = "{{.sid}}" }, "Action"]
  item      = "{{.action}}"
  unique_by = "value"
```

- **`show = "Sid"`**: statements are objects, so the picker lists them by their `Sid`.
- **`{ Sid = "{{.sid}}" }` in a key** picks the one list item whose `Sid` matches,
  then `Action` is the list inside it. The same works for HCL `jsonencode({...})`
  policies and for repeated `statement { }` blocks in `aws_iam_policy_document`.

**The PR**
```diff
       "Action": [
         "s3:GetObject",
-        "s3:ListBucket"
+        "s3:ListBucket",
+        "s3:PutObject"
       ],
```
