# backend.tf
#
# Remote state configuration.
#
# IMPORTANT: Fill in the values from the bootstrap outputs before running
# "terraform init" in this directory.
#
# The bucket, key, region, and dynamodb_table values are intentionally
# left as placeholders. They must be edited before use — Terraform does
# not support variable interpolation in backend blocks.
#
# Steps:
#   1. Run: cd terraform/bootstrap && terraform apply
#   2. Note the state_bucket_name and lock_table_name outputs.
#   3. Replace the placeholder values below with those outputs.
#   4. Run: cd .. && terraform init
#      Terraform will ask whether to copy existing local state to S3.
#      Answer "yes".

terraform {
  backend "s3" {
    # Replace with your actual bucket name from bootstrap output.
    bucket = "devops-sentinel-tfstate-123456789012"

    # The path within the bucket where this workspace's state is stored.
    # Using a path prefix makes it easy to store multiple workspaces in
    # one bucket: change "default" to a workspace name.
    key = "devops-sentinel/default/terraform.tfstate"

    # Must match the region used in bootstrap.
    region = "us-east-1"

    # Replace with your actual DynamoDB table name from bootstrap output.
    dynamodb_table = "devops-sentinel-tflock"

    # Encrypt state in transit and at rest.
    encrypt = true
  }
}
