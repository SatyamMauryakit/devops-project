# ---------------------------------------------------------------------------
# Global
# ---------------------------------------------------------------------------

variable "aws_region" {
  description = "AWS region for all resources."
  type        = string
  default     = "us-east-1"
}

variable "project" {
  description = "Short project name used as a prefix for all resource names and tags."
  type        = string
  default     = "devops-sentinel"
}

variable "environment" {
  description = "Deployment environment. Used in resource names and tags."
  type        = string
  default     = "dev"

  validation {
    condition     = contains(["dev", "staging", "prod"], var.environment)
    error_message = "environment must be one of: dev, staging, prod."
  }
}

variable "owner" {
  description = "Team or individual who owns these resources. Used in tags."
  type        = string
  default     = "platform-team"
}

# ---------------------------------------------------------------------------
# Network module
# ---------------------------------------------------------------------------

variable "vpc_cidr" {
  description = "CIDR block for the VPC. Change if this conflicts with your VPN."
  type        = string
  default     = "10.0.0.0/16"
}

variable "availability_zones" {
  description = "List of availability zones to use. Two AZs are required for EKS."
  type        = list(string)
  default     = ["us-east-1a", "us-east-1b"]
}

variable "public_subnet_cidrs" {
  description = "CIDR blocks for public subnets (one per AZ)."
  type        = list(string)
  default     = ["10.0.1.0/24", "10.0.2.0/24"]
}

variable "private_subnet_cidrs" {
  description = "CIDR blocks for private subnets (one per AZ). EKS nodes run here."
  type        = list(string)
  default     = ["10.0.10.0/24", "10.0.11.0/24"]
}

variable "single_nat_gateway" {
  description = <<-EOT
    When true, create one NAT Gateway in the first AZ only.
    Cost: ~$32/month (one NAT GW) vs ~$64/month (two NAT GWs for HA).

    Use true for dev/demo environments to save money.
    Use false for staging/prod where AZ failure tolerance is required.
  EOT
  type        = bool
  default     = true
}

# ---------------------------------------------------------------------------
# ECR module
# ---------------------------------------------------------------------------

variable "ecr_image_count" {
  description = "Maximum number of images to retain per ECR repository."
  type        = number
  default     = 10
}

# ---------------------------------------------------------------------------
# EKS module
# ---------------------------------------------------------------------------

variable "eks_enabled" {
  description = <<-EOT
    Set to true to create the EKS cluster and node group.
    Set to false to destroy only these resources after a demo while keeping
    ECR, IAM, and network resources intact.

    EKS control plane cost: ~$0.10/hour (~$73/month).
    Node group t3.medium cost: ~$0.047/hour (~$34/month) per node.
    Total: ~$107/month while enabled.
  EOT
  type        = bool
  default     = false
}

variable "eks_cluster_version" {
  description = "Kubernetes version for the EKS cluster."
  type        = string
  default     = "1.30"
}

variable "eks_node_instance_type" {
  description = "EC2 instance type for the EKS managed node group."
  type        = string
  default     = "t3.medium"
}

variable "eks_node_desired_count" {
  description = "Desired number of worker nodes."
  type        = number
  default     = 2
}

variable "eks_node_min_count" {
  description = "Minimum number of worker nodes."
  type        = number
  default     = 1
}

variable "eks_node_max_count" {
  description = "Maximum number of worker nodes."
  type        = number
  default     = 4
}

# ---------------------------------------------------------------------------
# IAM module
# ---------------------------------------------------------------------------

variable "ci_oidc_provider_url" {
  description = <<-EOT
    OIDC provider URL for CI (GitHub Actions).
    Used to create a federated IAM role that GitHub Actions can assume
    without long-lived access keys.
    Example: "token.actions.githubusercontent.com"
    Leave empty to skip the CI role.
  EOT
  type    = string
  default = "token.actions.githubusercontent.com"
}

variable "github_org" {
  description = "GitHub organisation or username. Used to scope the CI IAM role trust policy."
  type        = string
  default     = "your-github-org"
}

variable "github_repo" {
  description = "GitHub repository name. Used to scope the CI IAM role trust policy."
  type        = string
  default     = "devops-sentinel"
}
