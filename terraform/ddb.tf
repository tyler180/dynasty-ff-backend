module "dynamodb_table" {
  source  = "terraform-aws-modules/dynamodb-table/aws"
  version = "~> 5.5.0"

  name                           = "player-id"
  billing_mode                   = "PAY_PER_REQUEST"
  server_side_encryption_enabled = true
  point_in_time_recovery_enabled = true


  hash_key  = "pk"
  range_key = "sk"

  attributes = [
    {
      name = "pk"
      type = "S"
    },
    {
      name = "sk"
      type = "S"
    }
  ]

  tags = data.context_tags.backend.tags
}

module "player_game_stats_table" {
  source  = "terraform-aws-modules/dynamodb-table/aws"
  version = "~> 5.5.0"

  name                           = "${local.name}-player-game-stats"
  billing_mode                   = "PAY_PER_REQUEST"
  server_side_encryption_enabled = true
  point_in_time_recovery_enabled = true

  hash_key  = "pk"
  range_key = "sk"

  attributes = [
    {
      name = "pk"
      type = "S"
    },
    {
      name = "sk"
      type = "S"
    },
    {
      name = "gsi1pk"
      type = "S"
    },
    {
      name = "gsi1sk"
      type = "S"
    }
  ]

  global_secondary_indexes = [
    {
      name            = "season-index"
      hash_key        = "gsi1pk"
      range_key       = "gsi1sk"
      projection_type = "KEYS_ONLY"
    }
  ]

  tags = data.context_tags.backend.tags
}
