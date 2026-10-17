# versions.tf
# Standalone root module: provisions one EC2 instance that clones the
# devops-sentinel repo and runs it with docker-compose.
#
# Deliberately independent of ../main.tf (the EKS-oriented configuration).
# Uses local state — fine for a single operator / single instance. If you
# later need a team-shared state, point this at the same S3 backend set up
# in terraform/bootstrap.

terraform {
  required_version = ">= 1.8.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.55"
    }
    tls = {
      source  = "hashicorp/tls"
      version = "~> 4.0"
    }
  }
}

provider "aws" {
  region = var.aws_region

  default_tags {
    tags = {
      Project     = "devops-sentinel"
      Environment = "prod"
      ManagedBy   = "terraform"
      Module      = "ec2"
    }
  }
}
