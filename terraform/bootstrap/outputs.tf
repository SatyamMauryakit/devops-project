output "state_bucket_name" {
  description = "Name of the S3 bucket that stores Terraform state. Copy this into terraform/backend.tf."
  value       = aws_s3_bucket.tfstate.bucket
}

output "state_bucket_arn" {
  description = "ARN of the S3 state bucket."
  value       = aws_s3_bucket.tfstate.arn
}

output "lock_table_name" {
  description = "Name of the DynamoDB lock table. Copy this into terraform/backend.tf."
  value       = aws_dynamodb_table.tflock.name
}

output "lock_table_arn" {
  description = "ARN of the DynamoDB lock table."
  value       = aws_dynamodb_table.tflock.arn
}

output "next_step" {
  description = "Instructions for what to do after bootstrap apply completes."
  value       = <<-EOT
    Bootstrap complete.
    1. Copy the values above into terraform/backend.tf:
         bucket         = "${aws_s3_bucket.tfstate.bucket}"
         dynamodb_table = "${aws_dynamodb_table.tflock.name}"
         region         = "${var.aws_region}"
    2. cd ../  (the main terraform directory)
    3. terraform init
    4. terraform plan
  EOT
}
