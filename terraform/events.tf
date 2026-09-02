locals {
  mfl_sync_schedule_enabled = var.mfl_sync_schedule_expression != null
}

resource "aws_cloudwatch_event_rule" "mfl_sync" {
  count = local.mfl_sync_schedule_enabled ? 1 : 0

  name                = "${local.name}-mfl-sync"
  description         = "Read-only scheduled MFL league snapshot sync"
  schedule_expression = var.mfl_sync_schedule_expression

  tags = data.context_tags.backend.tags
}

resource "aws_cloudwatch_event_target" "mfl_sync" {
  count = local.mfl_sync_schedule_enabled ? 1 : 0

  rule = aws_cloudwatch_event_rule.mfl_sync[0].name
  arn  = module.ff_backend_lambda.lambda_function_arn
  input = jsonencode({
    action        = "sync_mfl"
    season        = var.mfl_sync_year
    league_id     = var.mfl_sync_league_id
    franchise_id  = var.mfl_sync_franchise_id
    include_draft = true
  })
}

resource "aws_lambda_permission" "mfl_sync" {
  count = local.mfl_sync_schedule_enabled ? 1 : 0

  statement_id  = "AllowExecutionFromEventBridge"
  action        = "lambda:InvokeFunction"
  function_name = module.ff_backend_lambda.lambda_function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.mfl_sync[0].arn
}

locals {
  nflverse_sync_schedule_enabled = var.nflverse_sync_schedule_expression != null
}

resource "aws_cloudwatch_event_rule" "nflverse_sync" {
  count = local.nflverse_sync_schedule_enabled ? 1 : 0

  name                = "${local.name}-nflverse-sync"
  description         = "Scheduled nflverse player-game statistics update check"
  schedule_expression = var.nflverse_sync_schedule_expression

  tags = data.context_tags.backend.tags
}

resource "aws_cloudwatch_event_target" "nflverse_sync" {
  count = local.nflverse_sync_schedule_enabled ? 1 : 0

  rule = aws_cloudwatch_event_rule.nflverse_sync[0].name
  arn  = module.ff_backend_lambda.lambda_function_arn
  input = jsonencode({
    action = "sync_snap_counts"
    season = var.nflverse_sync_year
  })
}

resource "aws_lambda_permission" "nflverse_sync" {
  count = local.nflverse_sync_schedule_enabled ? 1 : 0

  statement_id  = "AllowExecutionFromEventBridgeNFLVerse"
  action        = "lambda:InvokeFunction"
  function_name = module.ff_backend_lambda.lambda_function_name
  principal     = "events.amazonaws.com"
  source_arn    = aws_cloudwatch_event_rule.nflverse_sync[0].arn
}
