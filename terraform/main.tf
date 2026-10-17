# main.tf
# Root module: wires together all child modules.
# Each module is independently deployable; modules are listed in dependency order.

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = local.common_tags
  }
}

locals {
  # Tagging strategy applied to every resource in every module via default_tags.
  # Individual resources may add extra tags by merging with this map.
  common_tags = {
    Project     = var.project
    Environment = var.environment
    Owner       = var.owner
    ManagedBy   = "terraform"
    # Repository tag makes it easy to find which repo manages a resource.
    Repository  = "github.com/${var.github_org}/${var.github_repo}"
  }

  name_prefix = "${var.project}-${var.environment}"
}

# ---------------------------------------------------------------------------
# Network module
# Depends on: nothing
# Creates: VPC, subnets, IGW, NAT GW(s), route tables
# ---------------------------------------------------------------------------
module "network" {
  source = "./modules/network"

  project              = var.project
  environment          = var.environment
  vpc_cidr             = var.vpc_cidr
  availability_zones   = var.availability_zones
  public_subnet_cidrs  = var.public_subnet_cidrs
  private_subnet_cidrs = var.private_subnet_cidrs
  single_nat_gateway   = var.single_nat_gateway
  name_prefix          = local.name_prefix
}

# ---------------------------------------------------------------------------
# ECR module
# Depends on: nothing (ECR is a global service, no VPC needed)
# Creates: two ECR repositories with scanning and lifecycle policies
# ---------------------------------------------------------------------------
module "ecr" {
  source = "./modules/ecr"

  project     = var.project
  environment = var.environment
  name_prefix = local.name_prefix
  image_count = var.ecr_image_count
}

# ---------------------------------------------------------------------------
# IAM module
# Depends on: nothing (IAM is global)
# Creates: EKS cluster role, node role, CI deployment role
# ---------------------------------------------------------------------------
module "iam" {
  source = "./modules/iam"

  project              = var.project
  environment          = var.environment
  name_prefix          = local.name_prefix
  ci_oidc_provider_url = var.ci_oidc_provider_url
  github_org           = var.github_org
  github_repo          = var.github_repo
  ecr_repository_arns  = module.ecr.repository_arns
}

# ---------------------------------------------------------------------------
# EKS module
# Depends on: network (subnet IDs), iam (role ARNs)
# Conditionally created via eks_enabled variable.
# ---------------------------------------------------------------------------
module "eks" {
  source = "./modules/eks"

  # Pass false to skip creating all EKS resources.
  # All other variables are still validated; they just have no effect.
  enabled = var.eks_enabled

  project             = var.project
  environment         = var.environment
  name_prefix         = local.name_prefix
  cluster_version     = var.eks_cluster_version
  private_subnet_ids  = module.network.private_subnet_ids
  cluster_role_arn    = module.iam.eks_cluster_role_arn
  node_role_arn       = module.iam.eks_node_role_arn
  node_instance_type  = var.eks_node_instance_type
  node_desired_count  = var.eks_node_desired_count
  node_min_count      = var.eks_node_min_count
  node_max_count      = var.eks_node_max_count
  vpc_id              = module.network.vpc_id
}
