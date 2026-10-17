output "backend_repository_url" {
  description = "ECR URL for the backend image. Use as the image field in Kubernetes Deployments."
  value       = aws_ecr_repository.this["backend"].repository_url
}

output "frontend_repository_url" {
  description = "ECR URL for the frontend image."
  value       = aws_ecr_repository.this["frontend"].repository_url
}

output "repository_arns" {
  description = "Map of repository ARNs. Passed to the IAM module to build least-privilege policies."
  value = {
    backend  = aws_ecr_repository.this["backend"].arn
    frontend = aws_ecr_repository.this["frontend"].arn
  }
}

output "registry_id" {
  description = "AWS account ID owning the ECR registry. Used in docker login and image push commands."
  value       = aws_ecr_repository.this["backend"].registry_id
}
