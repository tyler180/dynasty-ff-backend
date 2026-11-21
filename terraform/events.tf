# MFL snapshots refresh around the league's blind-bid waiver runs: one sync
# before each run so free agents are current while bidding, and one shortly
# after it to pick up the results. EventBridge Scheduler is used instead of an
# EventBridge rule because it accepts a time zone, so the runs follow MFL's
# Eastern-time calendar through daylight saving changes.
resource "aws_iam_role" "mfl_sync_scheduler" {
  count = length(var.mfl_sync_schedules) > 0 ? 1 : 0

  name = "${local.name}-mfl-sync-scheduler"
  assume_role_policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect    = "Allow"
      Principal = { Service = "scheduler.amazonaws.com" }
      Action    = "sts:AssumeRole"
      Condition = { StringEquals = { "aws:SourceAccount" = data.aws_caller_identity.this.account_id } }
    }]
  })

  tags = data.context_tags.backend.tags
}

resource "aws_iam_role_policy" "mfl_sync_scheduler" {
  count = length(var.mfl_sync_schedules) > 0 ? 1 : 0

  name = "invoke-backend-lambda"
  role = aws_iam_role.mfl_sync_scheduler[0].id
  policy = jsonencode({
    Version = "2012-10-17"
    Statement = [{
      Effect   = "Allow"
      Action   = "lambda:InvokeFunction"
      Resource = module.ff_backend_lambda.lambda_function_arn
    }]
  })
}

resource "aws_scheduler_schedule" "mfl_sync" {
  for_each = var.mfl_sync_schedules

  name                         = "${local.name}-mfl-sync-${each.key}"
  description                  = "Read-only MFL team snapshot sync (${each.key})"
  schedule_expression          = each.value
  schedule_expression_timezone = var.mfl_sync_schedule_timezone

  flexible_time_window {
    mode = "OFF"
  }

  target {
    arn      = module.ff_backend_lambda.lambda_function_arn
    role_arn = aws_iam_role.mfl_sync_scheduler[0].arn
    input = jsonencode({
      action          = "sync_mfl"
      season          = var.mfl_sync_year
      league_id       = var.mfl_sync_league_id
      franchise_id    = var.mfl_sync_franchise_id
      include_draft   = true
      timeout_seconds = 300
    })

    retry_policy {
      maximum_event_age_in_seconds = 3600
      maximum_retry_attempts       = 2
    }
  }
}

locals {
  nflverse_sync_schedule_enabled = var.nflverse_sync_schedule_expression != null
}

resource "aws_cloudwatch_event_rule" "nflverse_sync" {
  count = local.nflverse_sync_schedule_enabled ? 1 : 0

  name                = "${local.name}-nflverse-sync"
  description         = "Scheduled nflverse player-game dataset update checks"
  schedule_expression = var.nflverse_sync_schedule_expression

  tags = data.context_tags.backend.tags
}

resource "aws_cloudwatch_event_target" "nflverse_sync" {
  count = local.nflverse_sync_schedule_enabled ? 1 : 0

  rule      = aws_cloudwatch_event_rule.nflverse_sync[0].name
  arn       = module.ff_backend_lambda.lambda_function_arn
  target_id = "snap-counts"
  input = jsonencode({
    action = "sync_snap_counts"
    season = var.nflverse_sync_year
  })
}

resource "aws_cloudwatch_event_target" "nflverse_player_stats_sync" {
  count = local.nflverse_sync_schedule_enabled ? 1 : 0

  rule      = aws_cloudwatch_event_rule.nflverse_sync[0].name
  arn       = module.ff_backend_lambda.lambda_function_arn
  target_id = "player-stats"
  input = jsonencode({
    action = "sync_player_stats"
    season = var.nflverse_sync_year
  })
}

resource "aws_cloudwatch_event_target" "nflverse_depth_charts_sync" {
  count = local.nflverse_sync_schedule_enabled ? 1 : 0

  rule      = aws_cloudwatch_event_rule.nflverse_sync[0].name
  arn       = module.ff_backend_lambda.lambda_function_arn
  target_id = "depth-charts"
  input = jsonencode({
    action = "sync_depth_charts"
    season = var.nflverse_depth_chart_sync_year
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
