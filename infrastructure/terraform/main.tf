module "ffnotifier_lambda_function" {
  source  = "terraform-aws-modules/lambda/aws"
  version = "7.14.0"

  function_name          = "fantasy-football-notifier"
  description            = "My awesome lambda function"
  handler                = "index.lambdaHandler"
  runtime                = "provided.al2023"
  ephemeral_storage_size = 10240
  architectures          = ["arm64"]
  publish                = true
  create_package         = false
  trusted_entities       = ["scheduler.amazonaws.com"]
  local_existing_package = "${path.module}/ffnotifier/ffnotifier.zip"

  source_path = "${path.module}/ffnotifier"

  #   store_on_s3 = true
  #   s3_bucket   = module.s3_bucket.s3_bucket_id
  #   s3_prefix   = "lambda-builds/"

  #   s3_object_override_default_tags = true
  #   s3_object_tags = {
  #     S3ObjectName = "lambda1"
  #     Override     = "true"
  #   }

  #   artifacts_dir = "${path.root}/.terraform/lambda-builds/"

  #   layers = [
  #     module.lambda_layer_local.lambda_layer_arn,
  #     module.lambda_layer_s3.lambda_layer_arn,
  #   ]

  environment_variables = {
    YEAR         = "2024"
    LEAGUE_ID    = "79286"
    FRANCHISE_ID = "0005"
  }

  # MFL credentials must be retrieved at runtime from AWS Secrets Manager.
  # Do not place usernames, passwords, cookies, or API keys in Terraform.

  cloudwatch_logs_log_group_class = "STANDARD"

  role_path   = "/tf-managed/"
  policy_path = "/tf-managed/"

  #   attach_dead_letter_policy = true
  #   dead_letter_target_arn    = aws_sqs_queue.dlq.arn

  allowed_triggers = {
    Config = {
      principal        = "config.amazonaws.com"
      principal_org_id = data.aws_organizations_organization.this.id
    },
    # APIGatewayAny = {
    #   service    = "apigateway"
    #   source_arn = "arn:aws:execute-api:eu-west-1:${data.aws_caller_identity.current.account_id}:aqnku8akd0/*/*/*"
    # },
    # APIGatewayDevPost = {
    #   service    = "apigateway"
    #   source_arn = "arn:aws:execute-api:eu-west-1:${data.aws_caller_identity.current.account_id}:aqnku8akd0/dev/POST/*"
    # },
    # OneRule = {
    #   principal  = "events.amazonaws.com"
    #   source_arn = "arn:aws:events:eu-west-1:${data.aws_caller_identity.current.account_id}:rule/RunDaily"
    # },
    FiveMinTrigger = {
      principal  = "scheduler.amazonaws.com"
      source_arn = module.eventbridge_scheduler.eventbridge_schedule_arns["lambda-cron"]
    }
  }

  ######################
  # Lambda Function URL
  ######################
  create_lambda_function_url = true
  authorization_type         = "AWS_IAM"
  cors = {
    allow_credentials = true
    allow_origins     = ["*"]
    allow_methods     = ["*"]
    allow_headers     = ["date", "keep-alive"]
    expose_headers    = ["keep-alive", "date"]
    max_age           = 86400
  }
  invoke_mode = "RESPONSE_STREAM"

  ######################
  # Additional policies
  ######################

  assume_role_policy_statements = {
    account_root = {
      effect  = "Allow",
      actions = ["sts:AssumeRole"],
      principals = {
        account_principal = {
          type        = "AWS",
          identifiers = ["arn:aws:iam::${data.aws_caller_identity.current.account_id}:root"]
        }
      }
      condition = {
        stringequals_condition = {
          test     = "StringEquals"
          variable = "sts:ExternalId"
          values   = ["12345"]
        }
      }
    }
  }

  attach_policy_json = true
  policy_json        = <<-EOT
    {
        "Version": "2012-10-17",
        "Statement": [
            {
                "Effect": "Allow",
                "Action": [
                    "xray:GetSamplingStatisticSummaries"
                ],
                "Resource": ["*"]
            }
        ]
    }
  EOT

  attach_policy_jsons = true
  policy_jsons = [
    <<-EOT
      {
          "Version": "2012-10-17",
          "Statement": [
              {
                  "Effect": "Allow",
                  "Action": [
                      "xray:*"
                  ],
                  "Resource": ["*"]
              }
          ]
      }
    EOT
  ]
  number_of_policy_jsons = 1

  attach_policy = true
  policy        = "arn:aws:iam::aws:policy/AWSXRayDaemonWriteAccess"

  attach_policies    = true
  policies           = ["arn:aws:iam::aws:policy/AWSXrayReadOnlyAccess"]
  number_of_policies = 1

  attach_policy_statements = true
  policy_statements = {
    dynamodb = {
      effect    = "Allow",
      actions   = ["dynamodb:BatchWriteItem"],
      resources = ["arn:aws:dynamodb:eu-west-1:052212379155:table/Test"]
    },
    s3_read = {
      effect    = "Deny",
      actions   = ["s3:HeadObject", "s3:GetObject"],
      resources = ["arn:aws:s3:::my-bucket/*"]
    }
  }

  timeout = 300
  timeouts = {
    create = "20m"
    update = "20m"
    delete = "20m"
  }

  function_tags = {
    Language = "Go"
  }

  tags = {
    Module = "lambda1"
  }
}
