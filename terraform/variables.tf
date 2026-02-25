variable "aws_region" {
  description = "AWS region"
  type        = string
  default     = "us-east-1"
}

variable "db_password" {
  description = "RDS master password"
  type        = string
  sensitive   = true
}

variable "infra_service_ip" {
  description = "Public IP of the infra service (Fly.io) allowed to reach RDS"
  type        = string
}

variable "s3_export_bucket" {
  description = "S3 bucket name for customer data exports"
  type        = string
  default     = "crimata-exports"
}
