# Change requests: two examples side by side

> Change requests are **planned, not built yet**. Everything here is valid today
> except the one line marked `# proposed` (`mode = "update"`). The idea is explained
> in [docs/changing-existing-resources.md](../../docs/changing-existing-resources.md).

| | [Helm: scale a service](helm-scale-service/) | [Terraform: upgrade EKS](terraform-eks-version/) |
|---|---|---|
| **The developer wants to** | run 4 copies of `payments` instead of 2 | move cluster `apps` from 1.32 to 1.33 |
| **File platformr owns** | `apps/payments/values-platformr.yaml` | `clusters/apps/instance.auto.tfvars` |
| **Files people own** | `values.yaml`, `application.yaml` | `main.tf` |
| **How the IaC picks it up** | Argo CD lists it last in `valueFiles` | Terraform loads `*.auto.tfvars` automatically |
| **Request in the picker** | Service › Scale service | EKS › Upgrade cluster version |
| **Asks** | which service, how many replicas | which cluster, which version |
| **Keeps as-is** | `cpu` (not asked) | `node_count` (not asked) |
| **PR diff** | 1 line | 1 line |

The two columns are configured the same way. Only names, paths and the template
contents differ, because those are the parts specific to Helm or Terraform.
platformr itself doesn't know which is which.

---

## 1. Repo layout

**Helm**
```
apps/payments/
├── application.yaml        people own (Argo CD Application)
├── values.yaml             people own
└── values-platformr.yaml   platformr owns
```

**Terraform**
```
clusters/apps/
├── main.tf                 people own
└── instance.auto.tfvars    platformr owns
```

## 2. The template for the owned file

The real file with blanks. `{{.replicas}}` is filled with the answer to the question
named `replicas`, and so on. This is the only place the file format appears.

**Helm**: `platformr/templates/service/values-platformr.yaml.tmpl`
```yaml
# Owned by platformr. Change it with a platformr request, not by hand.
replicaCount: {{.replicas}}
resources:
  requests:
    cpu: "{{.cpu}}"
```

**Terraform**: `platformr/templates/eks/instance.auto.tfvars.tmpl`
```hcl
# Owned by platformr. Change it with a platformr request, not by hand.
cluster_version = "{{.cluster_version}}"
node_count      = {{.node_count}}
```

## 3. The change request in `platformr.toml`

Each file also has a "New service" / "New cluster" request that creates
everything; see the full `platformr.toml` in each folder. The change request
points at **only** the owned template, and asks **only** what this action changes.

**Helm**
```toml
[[resources]]
name         = "service-scale"
category     = "Service"
display_name = "Scale service"
mode         = "update"                                   # proposed
template     = "platformr/templates/service/values-platformr.yaml.tmpl"
target_path  = "apps/{{.name}}/"
file_name    = "values-platformr"
file_ext     = ".yaml"
pr_title     = "chore(service): scale {{.name}} to {{.replicas}} replicas"

  [[resources.fields]]
  name   = "name"
  type   = "select"
  label  = "Which service?"
  source = "dirs:apps"            # lists the folders under apps/

  [[resources.fields]]
  name    = "replicas"
  type    = "select"
  label   = "How many copies (replicas)?"
  options = ["1", "2", "3", "4", "6"]
```

**Terraform**
```toml
[[resources]]
name         = "eks-upgrade"
category     = "EKS"
display_name = "Upgrade cluster version"
mode         = "update"                                   # proposed
template     = "platformr/templates/eks/instance.auto.tfvars.tmpl"
target_path  = "clusters/{{.name}}/"
file_name    = "instance"
file_ext     = ".auto.tfvars"
pr_title     = "chore(eks): upgrade {{.name}} to {{.cluster_version}}"

  [[resources.fields]]
  name   = "name"
  type   = "select"
  label  = "Which cluster?"
  source = "dirs:clusters"        # lists the folders under clusters/

  [[resources.fields]]
  name    = "cluster_version"
  type    = "select"
  label   = "Kubernetes version (one step at a time, e.g. 1.32 → 1.33)"
  options = ["1.32", "1.33", "1.34"]
```

Where `cpu` and `node_count` come from: they aren't asked, so platformr reuses the
answers recorded when the resource was created (or last changed).

## 4. What the developer sees

**Helm**
```
$ platformr request
? What type of resource?          › Service
? What would you like to request? › Scale service
? Which service?                  › payments
? How many copies (replicas)?     › 4   (current: 2)

Changing payments:
  replicas   2 → 4

? Open a pull request with this change? Yes
✓ PR opened
```

**Terraform**
```
$ platformr request
? What type of resource?          › EKS
? What would you like to request? › Upgrade cluster version
? Which cluster?                  › apps
? Kubernetes version              › 1.33   (current: 1.32)

Changing apps:
  cluster_version   1.32 → 1.33

? Open a pull request with this change? Yes
✓ PR opened
```

## 5. The PR

**Helm**: `chore(service): scale payments to 4 replicas`
```diff
 # Owned by platformr. Change it with a platformr request, not by hand.
-replicaCount: 2
+replicaCount: 4
 resources:
   requests:
     cpu: "500m"
```

**Terraform**: `chore(eks): upgrade apps to 1.33`
```diff
 # Owned by platformr. Change it with a platformr request, not by hand.
-cluster_version = "1.32"
+cluster_version = "1.33"
 node_count      = 3
```

The reviewer sees one line, the platform team applies it the usual way
(Argo CD syncs / `terraform apply`), and nothing else in the repo changed.

---

## Check it yourself

```
go test ./examples/changeable/ -v
```

- **Owned files match their templates:** each owned file equals its template
  filled with the original answers. That's the planned "edited by hand?" check.
- **Example configs parse:** both `platformr.toml` files load with today's config.
- **A change request changes only what was asked:** re-rendering with one new answer
  changes exactly one line, the diffs above.
