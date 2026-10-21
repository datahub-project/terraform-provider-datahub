terraform {
  required_version = ">= 1.11"
  required_providers {
    datahub = {
      source  = "datahub-project/datahub"
      version = "0.27.0"
    }
    aws = {
      source  = "hashicorp/aws"
      version = "~> 6.69" # write-only secret values need >= 5.88
    }
    http = {
      source  = "hashicorp/http"
      version = "~> 3.5"
    }
    random = {
      source  = "hashicorp/random"
      version = "~> 3.6"
    }
  }
}

provider "datahub" {
  # Credentials from environment:
  #   DATAHUB_GMS_URL   - e.g. https://your-instance.acryl.io
  #   DATAHUB_GMS_TOKEN - personal access token
}

provider "aws" {
  region = var.region

  default_tags {
    tags = var.default_tags
  }
}

# No kubernetes or helm provider: the executor runs as an ECS Fargate service
# created by the CloudFormation stack in ecs.tf, and CloudFormation is driven
# through the aws provider.
