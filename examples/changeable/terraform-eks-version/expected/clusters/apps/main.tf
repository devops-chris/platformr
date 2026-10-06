module "eks" {
  source  = "terraform-aws-modules/eks/aws"
  version = "~> 20.0"

  cluster_name    = "apps"
  cluster_version = "1.33"

  eks_managed_node_groups = {
    default = {
      instance_types = ["m6i.2xlarge"]
      desired_size   = 3
    }
  }
}
