# terraform {
#   required_version = ">= 1.0.0"

#   backend "s3" {
#     region  = "us-west-2"
#     bucket  = "ff-test-ffbackends-state"
#     key     = "terraform.tfstate"
#     profile = ""
#     encrypt = "true"

#     dynamodb_table = "ff-test-ffbackends-state-lock"
#   }
# }
