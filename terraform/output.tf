output "tags" {
  value = data.context_tags.backend.tags
}

output "label_id_rendered" {
  value = data.context_label.backend.rendered
}

output "config_values" {
  value = data.context_config.backend.values
}

output "dir_name" {
  value = basename(dirname(path.cwd))
}

output "player_identity_table_name" {
  value = module.dynamodb_table.dynamodb_table_id
}

output "player_game_stats_table_name" {
  description = "DynamoDB table containing normalized player-game statistics"
  value       = module.player_game_stats_table.dynamodb_table_id
}

output "league_data_bucket_name" {
  value = module.backend_bucket.s3_bucket_id
}

output "lambda_function_name" {
  value = module.ff_backend_lambda.lambda_function_name
}

output "http_api_url" {
  description = "Base URL for the authenticated HTTP API"
  value       = module.dynasty_ff_backend_apigateway.api_endpoint
}

output "http_api_routes" {
  description = "HTTP routes exposed by the API; only health is unauthenticated"
  value = {
    public        = ["GET /health"]
    authenticated = ["GET /v1/free-agents/defensive-trends", "GET /v1/players/snaps", "GET /v1/snapshots/at", "GET /v1/snapshots/latest", "POST /v1/analyze", "POST /v1/snapshots/sync"]
  }
}

output "mfl_secret_arn" {
  description = "Secrets Manager ARN containing MFL and FantasyPros provider credentials"
  value       = module.secrets_manager.secret_arn
}

output "source_path" {
  value = local.source_path
}

output "files" {
  value = local.files
}
