# A small but not-toy EKS cluster: two worker nodes in public subnets, the
# core addons, and the Pod Identity agent that lets the secrets-store CSI
# driver assume a role on the worker pods' behalf. Where the Azure example
# gets networking and the Key Vault CSI driver from AKS for free, EKS needs
# them spelled out; the two community modules below keep that to the
# essentials rather than ~250 lines of VPC and IAM boilerplate.
#
# Public subnets with public node IPs, and no NAT gateway: the nodes need
# outbound access only (the executor image registry, GMS, and PyPI for
# ingestion plugins), and a NAT gateway would add a resource and roughly
# USD 32/month for nothing the example demonstrates. Not a production shape.
module "vpc" {
  source  = "terraform-aws-modules/vpc/aws"
  version = "~> 6.7"

  name = local.cluster_name
  cidr = "10.0.0.0/16"

  # EKS requires subnets in at least two availability zones.
  azs            = slice(data.aws_availability_zones.available.names, 0, 2)
  public_subnets = ["10.0.1.0/24", "10.0.2.0/24"]

  map_public_ip_on_launch = true
  enable_nat_gateway      = false

  public_subnet_tags = {
    "kubernetes.io/role/elb" = 1
  }
}

module "eks" {
  source  = "terraform-aws-modules/eks/aws"
  version = "~> 21.29"

  name               = local.cluster_name
  kubernetes_version = var.kubernetes_version

  vpc_id     = module.vpc.vpc_id
  subnet_ids = module.vpc.public_subnets

  # The kubernetes/helm providers reach the API server from wherever terraform
  # runs, which the module's default (private endpoint only) would block.
  endpoint_public_access = true

  # Grants the identity running terraform cluster-admin, which the kubernetes
  # and helm providers need to create the namespace and releases.
  enable_cluster_creator_admin_permissions = true

  addons = {
    coredns    = {}
    kube-proxy = {}
    vpc-cni = {
      before_compute = true
    }
    # Serves credentials to pods that have an aws_eks_pod_identity_association;
    # the secrets-store CSI provider uses it to read Secrets Manager.
    eks-pod-identity-agent = {}
  }

  eks_managed_node_groups = {
    workers = {
      instance_types = [var.node_instance_type]
      min_size       = var.node_count
      max_size       = var.node_count
      desired_size   = var.node_count
    }
  }
}
