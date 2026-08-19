data "aws_iam_policy_document" "lambda_policy_doc" {
  statement {
    actions = [
      "dynamodb:GetItem",
      "dynamodb:BatchGetItem",
      "dynamodb:PutItem",
    ]
    resources = [module.dynamodb_table.dynamodb_table_arn]
  }

  statement {
    actions = [
      "s3:ListBucket",
    ]
    resources = [module.backend_bucket.s3_bucket_arn]
  }

  statement {
    actions = [
      "s3:GetObject",
      "s3:PutObject",
    ]
    resources = [
      "${module.backend_bucket.s3_bucket_arn}/snapshots/*",
    ]
  }

  statement {
    actions   = ["secretsmanager:GetSecretValue"]
    resources = [module.secrets_manager.secret_arn]
  }

  statement {
    actions   = ["lambda:InvokeFunction"]
    resources = ["arn:aws:lambda:${data.aws_region.current.region}:${data.aws_caller_identity.this.account_id}:function:${local.name}"]
  }
}
