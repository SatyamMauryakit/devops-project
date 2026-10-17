# ---------------------------------------------------------------------------
# Network outputs
# ---------------------------------------------------------------------------

output "vpc_id" {
  description = "ID of the VPC."
  value       = module.network.vpc_id
}

output "private_subnet_ids" {
  description = "IDs of the private subnets where EKS nodes run."
  value       = module.network.private_subnet_ids
}

output "public_subnet_ids" {
  description = "IDs of the public subnets used by load balancers."
  value       = module.network.public_subnet_ids
}

# ---------------------------------------------------------------------------
# ECR outputs
# ---------------------------------------------------------------------------

output "ecr_backend_url" {
  description = "ECR repository URL for the backend image. Use this as the image name in Kubernetes manifests."
  value       = module.ecr.backend_repository_url
}

output "ecr_frontend_url" {
  description = "ECR repository URL for the frontend image."
  value       = module.ecr.frontend_repository_url
}

output "ecr_registry_id" {
  description = "AWS account ID that owns the ECR registry. Used in docker login commands."
  value       = module.ecr.registry_id
}

# ---------------------------------------------------------------------------
# IAM outputs
# ---------------------------------------------------------------------------

output "ci_role_arn" {
  description = "ARN of the IAM role assumed by GitHub Actions. Add this to GitHub repository secrets as AWS_ROLE_ARN."
  value       = module.iam.ci_role_arn
}

# ---------------------------------------------------------------------------
# EKS outputs (empty strings when eks_enabled = false)
# ---------------------------------------------------------------------------

output "eks_cluster_name" {
  description = "EKS cluster name. Empty when eks_enabled = false."
  value       = module.eks.cluster_name
}

output "eks_cluster_endpoint" {
  description = "EKS API server endpoint. Empty when eks_enabled = false."
  value       = module.eks.cluster_endpoint
}

output "eks_cluster_ca" {
  description = "Base64-encoded certificate authority data for the EKS cluster."
  value       = module.eks.cluster_ca
  sensitive   = true
}

output "ecr_docker_login_command" {
  description = "Command to authenticate Docker with ECR. Run this before docker push."
  value       = "aws ecr get-login-password --region ${var.aws_region} | docker login --username AWS --password-stdin ${module.ecr.registry_id}.dkr.ecr.${var.aws_region}.amazonaws.com"
}
