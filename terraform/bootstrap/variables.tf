variable "aws_region" {
  description = "AWS region where the state bucket and lock table are created."
  type        = string
  default     = "us-east-1"
}

variable "project" {
  description = "Short project name. Used as a prefix for all resource names."
  type        = string
  default     = "devops-sentinel"
}

variable "environment" {
  description = "Environment label applied to all tags."
  type        = string
  default     = "shared"
}

variable "state_bucket_name" {
  description = <<-EOT
    Globally unique name for the S3 state bucket.
    S3 bucket names are globally unique across all AWS accounts.
    A common pattern: "<project>-tfstate-<aws_account_id>".
    Do not use your real account ID in version control.
  EOT
  type        = string
}

variable "lock_table_name" {
  description = "Name of the DynamoDB table used for Terraform state locking."
  type        = string
  default     = "devops-sentinel-tflock"
}
