# outputs.tf

output "public_ip" {
  description = "Public IP of the EC2 instance. Use for EC2_HOST GitHub secret and to reach the app."
  value       = aws_instance.app.public_ip
}

output "instance_id" {
  description = "EC2 instance ID."
  value       = aws_instance.app.id
}

output "ssh_private_key" {
  description = "Private half of the generated SSH key pair (PEM, PKCS1). Save with: terraform output -raw ssh_private_key > sentinel-deploy-key.pem"
  value       = tls_private_key.deploy.private_key_pem
  sensitive   = true
}

output "ssh_command" {
  description = "Ready-to-use SSH command once the key is saved locally."
  value       = "ssh -i sentinel-deploy-key.pem ec2-user@${aws_instance.app.public_ip}"
}
