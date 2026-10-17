# variables.tf

variable "aws_region" {
  description = "AWS region to deploy into."
  type        = string
  default     = "us-east-1"
}

variable "instance_type" {
  description = "EC2 instance type. t3.micro is Free Tier eligible for new accounts but has only 1GB RAM; if the docker build OOMs, use t3.small."
  type        = string
  default     = "t3.medium"
}

variable "allowed_ssh_cidr" {
  description = "CIDR allowed to SSH into the instance on port 22. Use your own IP/32, never 0.0.0.0/0."
  type        = string
}

variable "github_repo_url" {
  description = "HTTPS clone URL of the public GitHub repo, e.g. https://github.com//devops-sentinel.git"
  type        = string
}

variable "mongo_root_username" {
  description = "MongoDB root username for the production container."
  type        = string
  default     = "admin"
}

variable "mongo_root_password" {
  description = "MongoDB root password for the production container. Generate a strong random value; never reuse the local dev password."
  type        = string
  sensitive   = true
}
