terraform {
  required_version = ">= 1.15.0"
  backend "s3" {
    region       = "us-west-2"
    bucket       = "dynasty-ff"
    key          = "models.tfstate"
    encrypt      = true
    use_lockfile = true
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 6.5.0"
    }
    local = {
      source  = "hashicorp/local"
      version = ">= 1.3"
    }
  }
}

provider "aws" {
  region = "us-west-2" # Update to your preferred region
}
