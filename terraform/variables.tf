# variable "username" {
#   type = string
# }

variable "mfl_sync_schedule_expression" {
  description = "Optional EventBridge schedule expression for read-only MFL sync; null disables scheduling"
  type        = string
  default     = null
}

variable "mfl_sync_year" {
  description = "MFL season passed to scheduled syncs"
  type        = number
  default     = 2026
}

variable "mfl_sync_league_id" {
  description = "MFL league ID passed to scheduled syncs"
  type        = string
  default     = "79286"
}

variable "mfl_sync_franchise_id" {
  description = "MFL franchise ID passed to scheduled syncs"
  type        = string
  default     = "0005"
}

# variable "password" {
#   type = string
# }

# variable "api_key" {
#   type = string
# }

# variable "league_id" {
#   type = string
# }

# variable "franchise_id" {
#   type = string
# }

# variable "league_year" {
#   type    = string
#   default = "2026"
# }

variable "username" {
  sensitive = true
  type      = string
}

variable "password" {
  sensitive = true
  type      = string
}

variable "api_key" {
  sensitive = true
  type      = string
}

variable "fantasypros_api_key" {
  sensitive = true
  type      = string
}

variable "league_id" {
  type = string
}

variable "franchise_id" {
  type = string
}

variable "league_year" {
  type    = string
  default = "2025"
}

variable "setjson" {
  type    = string
  default = "1"
}

variable "setxml" {
  type    = string
  default = "0"
}
