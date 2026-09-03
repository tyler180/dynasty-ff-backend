data "aws_region" "current" {}

data "aws_caller_identity" "this" {}

data "aws_ecr_authorization_token" "token" {}

locals {
  source_path = abspath("${path.module}/../..")
  path_include = [
    "dynasty-ff-backend/cmd/**",
    "dynasty-ff-backend/config/**",
    "dynasty-ff-backend/data/**",
    "dynasty-ff-backend/docs/**",
    "dynasty-ff-backend/internal/**",
    "dynasty-ff-backend/Dockerfile",
    "dynasty-ff-backend/go.mod",
    "dynasty-ff-backend/go.sum",
    "dynasty-ff-models/analysis/**",
    "dynasty-ff-models/cmd/**",
    "dynasty-ff-models/config/**",
    "dynasty-ff-models/data/**",
    "dynasty-ff-models/docs/**",
    "dynasty-ff-models/draft/**",
    "dynasty-ff-models/internal/**",
    "dynasty-ff-models/usage/**",
    "dynasty-ff-models/go.mod",
    "dynasty-ff-models/go.sum",
    "mfl/mfl-mcp/cmd/**",
    "mfl/mfl-mcp/internal/**",
    "mfl/mfl-mcp/go.mod",
    "mfl/mfl-mcp/go.sum",
  ]
  # path_exclude = [
  #   "dynasty-ff-*/terraform/**",
  #   "dynasty-ff-*/.gitignore",
  #   "dynasty-ff-*/.git/**",
  #   "dynasty-ff-*/.vscode/**",
  #   "dynasty-ff-*/.terraform/**",
  #   "mfl/mfl-mcp/terraform/**",
  #   "mfl/mfl-mcp/.gitignore",
  #   "mfl/mfl-mcp/.git/**",
  #   "mfl/mfl-mcp/.vscode/**",
  #   "mfl/mfl-mcp/.terraform/**"
  # ]
  files_include = setunion([for f in local.path_include : fileset(local.source_path, f)]...)
  # files_exclude = setunion([for f in local.path_exclude : fileset(local.source_path, f)]...)
  # files         = sort(setsubtract(local.files_include, local.files_exclude))
  files = sort(local.files_include)

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
    PLAYER_IDENTITY_TABLE    = module.dynamodb_table.dynamodb_table_id
    PLAYER_GAME_STATS_TABLE  = module.player_game_stats_table.dynamodb_table_id
    LEAGUE_DATA_BUCKET       = module.backend_bucket.s3_bucket_id
    MFL_MCP_COMMAND          = "/var/task/mfl-mcp"
    MFL_SECRET_ARN           = module.secrets_manager.secret_arn
    MFL_YEAR                 = "2026"
    IDENTITY_SOURCE_URL      = "https://raw.githubusercontent.com/DynastyProcess/data/master/files/db_playerids.csv"
    SNAP_COUNTS_URL_TEMPLATE = "https://github.com/nflverse/nflverse-data/releases/download/snap_counts/snap_counts_%d.csv"
    PLAYER_STATS_URL_TEMPLATES = join(",", [
      "https://github.com/nflverse/nflverse-data/releases/download/player_stats/stats_player_week_%d.csv",
      "https://github.com/nflverse/nflverse-data/releases/download/stats_player/stats_player_week_%d.csv",
    ])
  }

  timeout     = 900
  memory_size = 1024

  # allowed_triggers = {
  #   MFLSync = {
  #     principal  = "events.amazonaws.com"
  #     source_arn = aws_cloudwatch_event_rule.mfl_sync.arn
  #     # source_arn = "arn:aws:events:us-west-2:${data.aws_caller_identity.current.account_id}:rule/${local.name}-mfl-sync"
  #   }
  #   AllowExecutionFromHTTPAPI = {
  #     principal  = "apigateway.amazonaws.com"
  #     source_arn = "${data.aws_apigatewayv2_api.backend.execution_arn}/*/*"
  #   }
  # }

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
  # image_tag     = "1.0"

  source_path      = local.source_path
  docker_file_path = "${local.source_path}/dynasty-ff-backend/Dockerfile"
  platform         = "linux/amd64"

  triggers = {
    dir_sha = local.dir_sha
  }
}
