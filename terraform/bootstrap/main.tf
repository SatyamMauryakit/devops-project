# bootstrap/main.tf
#
# PURPOSE
# -------
# This configuration creates the S3 bucket and DynamoDB table that the
# MAIN Terraform configuration uses as its remote backend.
#
# BOOTSTRAP ORDER (read before running anything else)
# ----------------------------------------------------
# 1. Run THIS configuration first with LOCAL state:
#      cd terraform/bootstrap
#      terraform init
#      terraform apply
#
# 2. The outputs print the bucket name and table name.
#
# 3. Copy those values into terraform/backend.tf.
#
# 4. Then run the MAIN configuration:
#      cd terraform
#      terraform init   # this time it migrates state to S3
#      terraform apply
#
# NEVER run "terraform destroy" in this directory while the main
# configuration still has remote state in the bucket. Destroy the
# main configuration first, then come back here to clean up.
#
# This configuration intentionally uses local state (no backend block).
# The bootstrap state file is tiny and low-risk. Keep it local and
# commit it to git only if you are certain no secrets are in it
# (there are none here).

terraform {
  required_version = ">= 1.8.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.55"
    }
  }
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = local.common_tags
  }
}

locals {
  common_tags = {
    Project     = var.project
    Environment = var.environment
    ManagedBy   = "terraform"
    Module      = "bootstrap"
  }
}

# ---------------------------------------------------------------------------
# S3 bucket for Terraform state
# ---------------------------------------------------------------------------
resource "aws_s3_bucket" "tfstate" {
  bucket = var.state_bucket_name

  # Prevent accidental deletion of the bucket that holds all Terraform state.
  lifecycle {
    prevent_destroy = true
  }
}

# Block all public access. State files contain sensitive infrastructure data.
resource "aws_s3_bucket_public_access_block" "tfstate" {
  bucket = aws_s3_bucket.tfstate.id

  block_public_acls       = true
  block_public_policy     = true
  ignore_public_acls      = true
  restrict_public_buckets = true
}

# Enable versioning so every state revision is recoverable.
resource "aws_s3_bucket_versioning" "tfstate" {
  bucket = aws_s3_bucket.tfstate.id

  versioning_configuration {
    status = "Enabled"
  }
}

# Encrypt state at rest with the default S3 managed key (SSE-S3).
# To use a customer-managed KMS key, replace sse_algorithm with "aws:kms"
# and provide a kms_master_key_id.
resource "aws_s3_bucket_server_side_encryption_configuration" "tfstate" {
  bucket = aws_s3_bucket.tfstate.id

  rule {
    apply_server_side_encryption_by_default {
      sse_algorithm = "AES256"
    }
  }
}

# Lifecycle rule: expire non-current state versions after 90 days to
# limit storage costs while retaining recent history.
resource "aws_s3_bucket_lifecycle_configuration" "tfstate" {
  bucket = aws_s3_bucket.tfstate.id

  rule {
    id     = "expire-old-versions"
    status = "Enabled"

    noncurrent_version_expiration {
      noncurrent_days = 90
    }
  }
}

# ---------------------------------------------------------------------------
# DynamoDB table for state locking
# ---------------------------------------------------------------------------
resource "aws_dynamodb_table" "tflock" {
  name         = var.lock_table_name
  billing_mode = "PAY_PER_REQUEST"  # Free tier: 25 WCU/25 RCU; locking uses negligible capacity.
  hash_key     = "LockID"

  attribute {
    name = "LockID"
    type = "S"
  }

  # Point-in-time recovery is cheap insurance. Locks are temporary but
  # accidental table deletion could prevent all Terraform operations.
  point_in_time_recovery {
    enabled = true
  }
}
