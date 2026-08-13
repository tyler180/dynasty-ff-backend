data "aws_region" "current" {}

data "aws_caller_identity" "this" {}

data "aws_ecr_authorization_token" "token" {}

locals {
  source_path = abspath("${path.module}/../..")
  path_include = [
    "dynasty-ff-backend/**",
    "dynasty-ff-models/**",
    "mfl/mfl-mcp/**",
  ]
  path_exclude = [
    "dynasty-ff-*/terraform/**",
    "dynasty-ff-*/.gitignore",
    "dynasty-ff-*/.git/**",
    "dynasty-ff-*/.vscode/**",
    "dynasty-ff-*/.terraform/**",
    "mfl/mfl-mcp/terraform/**",
    "mfl/mfl-mcp/.gitignore",
    "mfl/mfl-mcp/.git/**",
    "mfl/mfl-mcp/.vscode/**",
    "mfl/mfl-mcp/.terraform/**"
  ]
  files_include = setunion([for f in local.path_include : fileset(local.source_path, f)]...)
  files_exclude = setunion([for f in local.path_exclude : fileset(local.source_path, f)]...)
  files         = sort(setsubtract(local.files_include, local.files_exclude))

  dir_sha = sha1(join("", [for f in local.files : filesha1("${local.source_path}/${f}")]))
}

module "ff_backend_lambda" {
  source  = "terraform-aws-modules/lambda/aws"
  version = "8.8.0"

  function_name  = local.name
  description    = "Dynasty Fantasy Football Backend Lambda Function"
  create_package = false

  image_uri    = module.ff_backend_build.image_uri
  package_type = "Image"

  environment_variables = {
    PLAYER_IDENTITY_TABLE = module.dynamodb_table.dynamodb_table_id
    LEAGUE_DATA_BUCKET    = module.backend_bucket.s3_bucket_id
    MFL_MCP_COMMAND       = "/var/task/mfl-mcp"
    MFL_SECRET_ARN        = module.secrets_manager.secret_arn
    IDENTITY_SOURCE_URL   = "https://raw.githubusercontent.com/DynastyProcess/data/master/files/db_playerids.csv"
  }

  timeout = 300

  attach_cloudwatch_logs_policy = true
  attach_policy_json            = true
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
