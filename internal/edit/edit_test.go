package edit_test

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/devops-chris/platformr/internal/edit"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden from current output")

// run applies ops to src and compares the change summaries plus the final file with
// testdata/<golden>.golden. Run `go test ./internal/edit -update` after reviewing a
// deliberate output change.
func run(t *testing.T, golden string, f edit.Format, name, src string, ops ...edit.Op) {
	t.Helper()
	d, err := edit.Open(f, name, []byte(src))
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, op := range ops {
		ch, err := d.Apply(op)
		if err != nil {
			t.Fatalf("%s %s: %v", op.Action, op.Path, err)
		}
		fmt.Fprintf(&b, "%s\n", ch)
	}
	fmt.Fprintf(&b, "----- %s -----\n%s", name, d.Bytes())
	got := b.String()
	path := filepath.Join("testdata", golden+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%v (run with -update to create it)", err)
	}
	if got != string(want) {
		t.Errorf("%s changed:\n--- got ---\n%s\n--- want ---\n%s", golden, got, want)
	}
}

func P(k any) edit.Path {
	p, err := edit.ParsePath(k, nil)
	if err != nil {
		panic(err)
	}
	return p
}

func TestYAML(t *testing.T) {
	run(t, "yaml_values", edit.YAML, "values.yaml", `# Owned by people
replicaCount: 2   # how many
image:
  repository: ghcr.io/acme/payments
  tag: "1.4.0"
resources:
  requests:
    cpu: 500m
    memory: '512Mi'
env:
  - name: LOG_LEVEL
    value: info
tolerations: []
ports: [80, 443]
`,
		edit.Op{Action: "set", Path: P("replicaCount"), Value: "4"},
		edit.Op{Action: "set", Path: P("resources.requests.cpu"), Value: "1"},
		edit.Op{Action: "set", Path: P("resources.requests.memory"), Value: "1Gi"},
		edit.Op{Action: "set", Path: P("image.tag"), Value: "1.5.0"},
		edit.Op{Action: "append", Path: P("env"), Item: edit.Item{Fields: map[string]string{"name": "REGION", "value": "us-east-1"}}},
		edit.Op{Action: "append", Path: P("ports"), Item: edit.Item{Value: "8080"}},
		edit.Op{Action: "append", Path: P("tolerations"), Item: edit.Item{Value: "gpu"}},
		edit.Op{Action: "remove", Path: P("ports"), Match: map[string]string{"value": "80"}},
	)
	run(t, "yaml_access", edit.YAML, "access.yaml", `users:
  - name: jane
    role: admin
  - name: raj
    role: read-only
teams:
  platform: admin
`,
		edit.Op{Action: "append", Path: P("users"), Item: edit.Item{Fields: map[string]string{"role": "read-only", "name": "lee"}}},
		edit.Op{Action: "remove", Path: P("users"), Match: map[string]string{"name": "jane"}},
		edit.Op{Action: "put", Path: P("teams"), Name: "data", Item: edit.Item{Value: "read-only"}},
		edit.Op{Action: "delete", Path: P("teams"), Name: "platform"},
		edit.Op{Action: "remove", Path: P("users"), Match: map[string]string{"name": "raj"}},
		edit.Op{Action: "remove", Path: P("users"), Match: map[string]string{"name": "lee"}},
	)
}

func TestJSON(t *testing.T) {
	run(t, "json_policy", edit.JSON, "policy.json", `{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Sid": "S3Read",
      "Effect": "Allow",
      "Action": [
        "s3:GetObject",
        "s3:ListBucket"
      ],
      "Resource": ["arn:aws:s3:::reports", "arn:aws:s3:::reports/*"]
    },
    {
      "Sid": "Logs",
      "Effect": "Allow",
      "Action": "logs:PutLogEvents",
      "Resource": "*"
    }
  ],
  "Tags": {"team": "data"},
  "MaxSessionSeconds": 3600
}
`,
		edit.Op{Action: "append", Path: P([]any{"Statement", map[string]any{"Sid": "S3Read"}, "Action"}), Item: edit.Item{Value: "s3:PutObject"}},
		edit.Op{Action: "append", Path: P([]any{"Statement", map[string]any{"Sid": "S3Read"}, "Resource"}), Item: edit.Item{Value: "arn:aws:s3:::exports"}},
		edit.Op{Action: "remove", Path: P([]any{"Statement", map[string]any{"Sid": "S3Read"}, "Action"}), Match: map[string]string{"value": "s3:ListBucket"}},
		edit.Op{Action: "set", Path: P("MaxSessionSeconds"), Value: "7200"},
		edit.Op{Action: "put", Path: P("Tags"), Name: "owner", Item: edit.Item{Value: "platform"}},
		edit.Op{Action: "append", Path: P("Statement"), Item: edit.Item{Fields: map[string]string{"Sid": "KMS", "Effect": "Allow", "Action": "kms:Decrypt", "Resource": "*"}}},
		edit.Op{Action: "remove", Path: P("Statement"), Match: map[string]string{"Sid": "Logs"}},
	)
}

func TestHCL(t *testing.T) {
	run(t, "hcl_main", edit.HCL, "main.tf", `locals {
  cluster_version = "1.32"
  node_count      = 3
  allowed_cidrs = [
    "10.0.0.0/8",
    "203.0.113.10/32",
  ]
  access_entries = {
    jane = {
      principal_arn = "arn:aws:iam::123456789012:role/jane"
      access_level  = "admin"
    }
  }
  tags = { team = "platform" }
}

module "eks" {
  source          = "terraform-aws-modules/eks/aws"
  cluster_version = local.cluster_version
}

resource "aws_security_group_rule" "https" {
  type        = "ingress"
  from_port   = 443
  to_port     = 443
  protocol    = "tcp"
  cidr_blocks = ["10.0.0.0/8"]
}

resource "aws_iam_policy" "reports" {
  name = "reports"
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [
      {
        Sid      = "S3Read"
        Effect   = "Allow"
        Action   = ["s3:GetObject"]
        Resource = "*"
      },
    ]
  })
}

data "aws_iam_policy_document" "ci" {
  statement {
    sid       = "ECRPush"
    actions   = ["ecr:PutImage"]
    resources = ["*"]
  }
  statement {
    sid       = "Logs"
    actions   = ["logs:PutLogEvents"]
    resources = ["*"]
  }
}
`,
		edit.Op{Action: "set", Path: P("locals.cluster_version"), Value: "1.33"},
		edit.Op{Action: "set", Path: P("locals.node_count"), Value: "5"},
		edit.Op{Action: "append", Path: P("locals.allowed_cidrs"), Item: edit.Item{Value: "198.51.100.7/32"}},
		edit.Op{Action: "remove", Path: P("locals.allowed_cidrs"), Match: map[string]string{"value": "10.0.0.0/8"}},
		edit.Op{Action: "put", Path: P("locals.access_entries"), Name: "raj", Item: edit.Item{Fields: map[string]string{"access_level": "read-only", "principal_arn": "arn:aws:iam::123456789012:role/raj"}}},
		edit.Op{Action: "delete", Path: P("locals.access_entries"), Name: "jane"},
		edit.Op{Action: "put", Path: P("locals.tags"), Name: "owner", Item: edit.Item{Value: "devops"}},
		edit.Op{Action: "append", Path: P("resource.aws_security_group_rule.https.cidr_blocks"), Item: edit.Item{Value: "172.16.0.0/12"}},
		edit.Op{Action: "set", Path: P("resource.aws_security_group_rule.https.from_port"), Value: "8443"},
		edit.Op{Action: "append", Path: P([]any{"resource", "aws_iam_policy", "reports", "policy", "Statement", map[string]any{"Sid": "S3Read"}, "Action"}), Item: edit.Item{Value: "s3:PutObject"}},
		edit.Op{Action: "append", Path: P([]any{"data", "aws_iam_policy_document", "ci", "statement", map[string]any{"sid": "ECRPush"}, "actions"}), Item: edit.Item{Value: "ecr:UploadLayerPart"}},
	)
	// errors
	d, _ := edit.Open(edit.HCL, "x.tf", []byte("locals {\n  a = merge(local.b, local.c)\n  v = var.x\n}\n"))
	_, err := d.Apply(edit.Op{Action: "put", Path: P("locals.a"), Name: "k", Item: edit.Item{Value: "v"}})
	wantErr(t, err, "isn't written as a plain value")
	_, err = d.Apply(edit.Op{Action: "set", Path: P("locals.v"), Value: "1"})
	wantErr(t, err, "isn't written as a plain value")
	_, err = d.Apply(edit.Op{Action: "set", Path: P("locals.nope"), Value: "1"})
	wantErr(t, err, "locals.nope not found")
	run(t, "hcl_toset", edit.HCL, "accounts.tf", `locals {
  pt_shared_prod_accounts = toset([
    "pt-shared-prod-infra",
    "pt-shared-prod-services",
  ])
}
`, edit.Op{Action: "append", Path: P("locals.pt_shared_prod_accounts"), Item: edit.Item{Value: "pt-shared-prod-ai"}})
	run(t, "hcl_empty_lists", edit.HCL, "group.tf", `locals {
  acme_non_prod_accounts = toset([
  ])
  acme_prod_accounts = toset([])
}
`,
		edit.Op{Action: "append", Path: P("locals.acme_non_prod_accounts"), Item: edit.Item{Value: "pt-ortho-acme-dev-services"}},
		edit.Op{Action: "append", Path: P("locals.acme_prod_accounts"), Item: edit.Item{Value: "pt-ortho-acme-prod-services"}},
	)
	run(t, "hcl_tfvars", edit.HCL, "terraform.tfvars", "cluster_version = \"1.32\"\nnode_count      = 3\n", edit.Op{Action: "set", Path: P("cluster_version"), Value: "1.33"})
}

func TestLines(t *testing.T) {
	run(t, "env", edit.Env, ".env", "# app\nexport LOG_LEVEL=info\nREPLICAS=2\nNAME=\"payments api\"\n[db]\nhost = db.internal\n",
		edit.Op{Action: "set", Path: P("REPLICAS"), Value: "4"},
		edit.Op{Action: "set", Path: P("LOG_LEVEL"), Value: "debug"},
		edit.Op{Action: "set", Path: P("NAME"), Value: "payments"},
		edit.Op{Action: "set", Path: P("db.host"), Value: "db2.internal"},
	)
	run(t, "marker_ts", edit.Marker, "index.ts", `const cluster = new eks.Cluster(this, "apps", {
  version: "1.32", // platformr:cluster_version
  nodeCount: 3, // platformr:node_count
  nodeCountMax: 6, // platformr:node_count_max
});
`,
		edit.Op{Action: "set", Path: P("marker:cluster_version"), Value: "1.33"},
		edit.Op{Action: "set", Path: P("marker:node_count"), Value: "5"},
	)
	run(t, "marker_bicep", edit.Marker, "main.bicepparam", "param skuName = 'P1v3' // platformr:sku\nparam zoneRedundant = false // platformr:zr\n",
		edit.Op{Action: "set", Path: P("marker:sku"), Value: "P2v3"},
		edit.Op{Action: "set", Path: P("marker:zr"), Value: "true"},
	)
}

func wantErr(t *testing.T, err error, contains string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), contains) {
		t.Errorf("err = %v, want it to contain %q", err, contains)
	}
}
