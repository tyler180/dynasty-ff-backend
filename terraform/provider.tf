terraform {
  required_version = ">= 1.15.0"
  backend "s3" {
    region       = "us-west-2"
    bucket       = "749rmw-tf-backends"
    key          = "dynasty-ff/backend.tfstate"
    encrypt      = true
    use_lockfile = true
  }

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 6.5.0"
    }
    docker = {
      source  = "kreuzwerker/docker"
      version = "4.5.0"
    }
    local = {
      source  = "hashicorp/local"
      version = ">= 1.3"
    }
    context = {
      source  = "registry.terraform.io/cloudposse/context"
      version = "0.5.0"
    }
  }
}

provider "aws" {
  region = "us-west-2" # Update to your preferred region
}

provider "docker" {
  registry_auth {
    address  = format("%v.dkr.ecr.%v.amazonaws.com", data.aws_caller_identity.this.account_id, data.aws_region.current.region)
    username = data.aws_ecr_authorization_token.token.user_name
    password = data.aws_ecr_authorization_token.token.password
  }
}

provider "context" {
  delimiter       = "-"
  enabled         = true
  tags_key_case   = "title"
  tags_value_case = "none"

  properties = {
    namespace = {
      required        = false
      include_in_tags = true
      tags_value_case = "lower"
    }
    environment = {
      required        = true
      include_in_tags = true
      tags_value_case = "lower"
    }
    stage = {
      required        = false
      include_in_tags = true
      tags_value_case = "lower"
    }
    name = {
      required        = true
      include_in_tags = true
      tags_value_case = "lower"
    }
  }

  values = {
    environment = "dev"
    name        = basename(dirname(path.cwd))
    # name = abspath("${path.module}/..")
  }
}

data "context_tags" "backend" {}

data "context_label" "backend" {
  template = "${data.context_tags.backend.tags["Name"]}-${data.context_tags.backend.tags["Environment"]}"
}

data "context_config" "backend" {}