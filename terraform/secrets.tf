# data "aws_caller_identity" "current" {}

# module "secrets-manager" {
#   source  = "terraform-aws-modules/secrets-manager/aws"
#   version = "1.3.1"

#   # Secret
#   name_prefix             = "mfl-secrets"
#   description             = "Secret containing MFL info"
#   recovery_window_in_days = 0
#   #   replica = {
#   #     # Can set region as key
#   #     us-east-1 = {}
#   #     another = {
#   #       # Or as attribute
#   #       region = "us-west-2"
#   #     }
#   #   }

#   # Policy
#   create_policy       = true
#   block_public_policy = true
#   policy_statements = {
#     read = {
#       sid = "AllowAccountRead"
#       principals = [{
#         type        = "AWS"
#         identifiers = ["arn:aws:iam::${data.aws_caller_identity.current.account_id}:root"]
#       }]
#       actions   = ["secretsmanager:GetSecretValue"]
#       resources = ["*"]
#     }
#   }

#   # Version
#   ignore_secret_changes = true
#   secret_string = jsonencode({
#     username     = var.username,
#     password     = var.password,
#     api_key      = var.api_key,
#     league_id    = var.league_id
#     franchise_id = var.franchise_id,
#     league_year  = var.league_year
#   })

#   tags = local.tags
# }
