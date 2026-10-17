variable "project"     { type = string }
variable "environment" { type = string }
variable "name_prefix" { type = string }

variable "ci_oidc_provider_url" {
  description = "OIDC provider URL for GitHub Actions federated identity. Set to empty string to skip CI role creation."
  type        = string
  default     = "token.actions.githubusercontent.com"
}

variable "github_org" {
  description = "GitHub organisation or username. Scopes the CI role trust policy to your org."
  type        = string
}

variable "github_repo" {
  description = "GitHub repository name. Scopes the CI role trust policy to your repo."
  type        = string
}

variable "ecr_repository_arns" {
  description = "Map of ECR repository ARNs. Used to build the least-privilege push policy for the CI role."
  type        = map(string)
  default     = {}
}
