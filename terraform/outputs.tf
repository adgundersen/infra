output "customer_security_group_id" {
  description = "Security group ID for customer EC2 instances"
  value       = aws_security_group.customer_ec2.id
}

output "ec2_iam_instance_profile" {
  description = "IAM instance profile name for customer EC2 instances"
  value       = aws_iam_instance_profile.crimata_ec2.name
}

output "s3_export_bucket" {
  description = "S3 bucket for customer data exports"
  value       = aws_s3_bucket.exports.bucket
}

output "rds_endpoint" {
  description = "RDS connection endpoint (host:port)"
  value       = aws_db_instance.crimata.endpoint
}

output "database_url" {
  description = "Full DATABASE_URL for the infra service"
  value       = "postgresql://crimata:${var.db_password}@${aws_db_instance.crimata.endpoint}/crimata"
  sensitive   = true
}
