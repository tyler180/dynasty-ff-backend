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

output "league_data_bucket_name" {
  value = module.backend_bucket.s3_bucket_id
}

output "lambda_function_name" {
  value = module.ff_backend_lambda.lambda_function_name
}

output "mfl_secret_arn" {
  description = "Populate this secret with api_key or user_cookie JSON after apply"
  value       = module.secrets_manager.secret_arn
}

output "source_path" {
  value = local.source_path
}

output "files" {
  value = local.files
}
