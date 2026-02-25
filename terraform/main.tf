terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region = var.aws_region
}

# ── Data: default VPC ──────────────────────────────────────────────────────────
data "aws_vpc" "default" {
  default = true
}

data "aws_subnets" "default" {
  filter {
    name   = "vpc-id"
    values = [data.aws_vpc.default.id]
  }
}

# ── Security group: customer EC2 instances ─────────────────────────────────────
resource "aws_security_group" "customer_ec2" {
  name        = "customer"
  description = "Customer EC2 instances"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    from_port   = 80
    to_port     = 80
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  ingress {
    from_port   = 443
    to_port     = 443
    protocol    = "tcp"
    cidr_blocks = ["0.0.0.0/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# ── IAM role for customer EC2 instances (SSM access) ──────────────────────────
resource "aws_iam_role" "crimata_ec2" {
  name = "crimata-ec2"

  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "ec2.amazonaws.com" }
      Action    = "sts:AssumeRole"
    }]
  })
}

resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.crimata_ec2.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_role_policy_attachment" "s3" {
  role       = aws_iam_role.crimata_ec2.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonS3FullAccess"
}

resource "aws_iam_instance_profile" "crimata_ec2" {
  name = "crimata-ec2-profile"
  role = aws_iam_role.crimata_ec2.name
}

# ── S3 bucket for data exports ─────────────────────────────────────────────────
resource "aws_s3_bucket" "exports" {
  bucket = var.s3_export_bucket
}

resource "aws_s3_bucket_lifecycle_configuration" "exports" {
  bucket = aws_s3_bucket.exports.id

  rule {
    id     = "expire-exports"
    status = "Enabled"

    filter {}

    expiration {
      days = 30
    }
  }
}

# ── Security group: RDS ────────────────────────────────────────────────────────
resource "aws_security_group" "rds" {
  name        = "crimata-rds"
  description = "Allow Postgres from infra service"
  vpc_id      = data.aws_vpc.default.id

  ingress {
    description = "Postgres from infra service"
    from_port   = 5432
    to_port     = 5432
    protocol    = "tcp"
    cidr_blocks = ["${var.infra_service_ip}/32"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }
}

# ── RDS subnet group ───────────────────────────────────────────────────────────
resource "aws_db_subnet_group" "crimata" {
  name       = "crimata"
  subnet_ids = data.aws_subnets.default.ids
}

# ── RDS instance ───────────────────────────────────────────────────────────────
resource "aws_db_instance" "crimata" {
  identifier        = "crimata"
  engine            = "postgres"
  engine_version    = "16"
  instance_class    = "db.t3.micro"
  allocated_storage = 20
  storage_type      = "gp2"

  db_name  = "crimata"
  username = "crimata"
  password = var.db_password

  db_subnet_group_name   = aws_db_subnet_group.crimata.name
  vpc_security_group_ids = [aws_security_group.rds.id]

  publicly_accessible       = true
  skip_final_snapshot       = false
  final_snapshot_identifier = "crimata-final"
  deletion_protection       = true

  backup_retention_period = 0
  maintenance_window      = "Mon:04:00-Mon:05:00"
}
