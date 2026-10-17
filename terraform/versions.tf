terraform {
  required_version = ">= 1.8.0"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.55"
    }
    tls = {
      # Used by the EKS module to create the OIDC provider certificate thumbprint.
      source  = "hashicorp/tls"
      version = "~> 4.0"
    }
  }
}
