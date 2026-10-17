# modules/ecr/main.tf
#
# Creates two private ECR repositories — one for the backend image and one
# for the frontend image — with:
#   - Image scanning on every push (detects known CVEs at no extra cost)
#   - Immutable tags (prevents overwriting a pushed tag, enforcing the
#     "never overwrite a released image" discipline)
#   - Lifecycle policy: keep the N most recent images, expire the rest
#     (controls storage cost; free tier is 500 MB/month per account)
#
# Cost: ECR storage is free for the first 500 MB/month per account.
# For small development images (<100 MB each) this is effectively free.

locals {
  repositories = {
    backend  = "${var.name_prefix}-backend"
    frontend = "${var.name_prefix}-frontend"
  }

  # Lifecycle policy JSON: keep only the N most recent tagged images.
  # Untagged images (build cache layers) are expired after 1 day.
  lifecycle_policy = jsonencode({
    rules = [
      {
        rulePriority = 1
        description  = "Expire untagged images after 1 day"
        selection = {
          tagStatus   = "untagged"
          countType   = "sinceImagePushed"
          countUnit   = "days"
          countNumber = 1
        }
        action = { type = "expire" }
      },
      {
        rulePriority = 2
        description  = "Keep only the ${var.image_count} most recent tagged images"
        selection = {
          tagStatus     = "tagged"
          tagPrefixList = ["v", "sha-", "latest"]
          countType     = "imageCountMoreThan"
          countNumber   = var.image_count
        }
        action = { type = "expire" }
      }
    ]
  })
}

resource "aws_ecr_repository" "this" {
  for_each = local.repositories

  name                 = each.value
  image_tag_mutability = "IMMUTABLE"

  image_scanning_configuration {
    # Basic scanning runs on every push at no charge.
    # Enhanced scanning (powered by Inspector) costs extra and is not
    # enabled here. Upgrade when you need OS-level and language CVE coverage.
    scan_on_push = true
  }

  # Encrypt images at rest with the default AWS-managed ECR key.
  encryption_configuration {
    encryption_type = "AES256"
  }

  tags = {
    Name      = each.value
    Component = each.key
  }
}

resource "aws_ecr_lifecycle_policy" "this" {
  for_each   = aws_ecr_repository.this
  repository = each.value.name
  policy     = local.lifecycle_policy
}
