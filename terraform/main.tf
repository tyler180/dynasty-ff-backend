data "aws_region" "current" {}

data "aws_caller_identity" "this" {}

data "aws_ecr_authorization_token" "token" {}

module "ff_backend_lambda" {
  source  = "terraform-aws-modules/lambda/aws"
  version = "8.8.0"

  function_name  = local.name
  description    = "Dynasty Fantasy Football Backend Lambda Function"
  create_package = false

  image_uri    = module.ff_backend_build.image_uri
  package_type = "Image"

  environment_variables = {
    PLAYER_IDENTITY_TABLE = "player-id"
    LEAGUE_DATA_BUCKET    = local.name
  }

  attach_cloudwatch_logs_policy = true
  policy_json                   = data.aws_iam_policy_document.lambda_policy_doc.json

  tags = data.context_tags.backend.tags
}

module "ff_backend_build" {
  source  = "terraform-aws-modules/lambda/aws//modules/docker-build"
  version = "8.8.0"

  create_ecr_repo = true
  ecr_repo        = local.name
  ecr_repo_lifecycle_policy = jsonencode({
    "rules" : [
      {
        "rulePriority" : 1,
        "description" : "Keep only the last 5 images",
        "selection" : {
          "tagStatus" : "any",
          "countType" : "imageCountMoreThan",
          "countNumber" : 5
        },
        "action" : {
          "type" : "expire"
        }
      }
    ]
  })

  use_image_tag = false # If false, sha of the image will be used

  # use_image_tag = true
  # image_tag   = "2.0"

  source_path      = local.source_path
  docker_file_path = "${local.source_path}/dynasty-ff-backend/Dockerfile"
  platform         = "linux/amd64"

  triggers = {
    dir_sha = local.dir_sha
  }
}
