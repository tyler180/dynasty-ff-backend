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

variable "nflverse_sync_schedule_expression" {
  description = "EventBridge schedule for checking the current nflverse snap-count dataset; null disables scheduling"
  type        = string
  default     = "rate(1 day)"
}

variable "nflverse_sync_year" {
  description = "NFL season checked by the scheduled nflverse sync"
  type        = number
  default     = 2026

  validation {
    condition     = var.nflverse_sync_year >= 2012 && var.nflverse_sync_year <= 2100
    error_message = "nflverse_sync_year must be between 2012 and 2100."
  }
}

variable "api_jwt_issuer" {
  description = "HTTPS issuer URL for the OIDC provider whose JWTs may call the read-only HTTP API"
  type        = string
  default     = "https://auth.k8s.749rmw.com/application/o/dynasty-ff/"

  validation {
    condition     = startswith(var.api_jwt_issuer, "https://")
    error_message = "api_jwt_issuer must be an HTTPS URL."
  }
}

variable "api_jwt_audiences" {
  description = "Allowed JWT audience values for the HTTP API"
  type        = set(string)
  default     = ["dynasty-ff-frontend"]

  validation {
    condition     = length(var.api_jwt_audiences) > 0 && alltrue([for audience in var.api_jwt_audiences : trimspace(audience) != ""])
    error_message = "api_jwt_audiences must contain at least one non-empty audience."
  }
}

variable "api_jwt_required_scopes" {
  description = "Optional OAuth scopes required on authenticated HTTP API routes"
  type        = set(string)
  default     = []
}

variable "api_allowed_origins" {
  description = "Exact browser origins allowed to call the HTTP API; wildcard origins are rejected because credentials are enabled"
  type        = set(string)
  default = [
    "https://dynasty-ff.749rmw.com",
    "http://localhost:3000",
  ]

  validation {
    condition     = length(var.api_allowed_origins) > 0 && alltrue([for origin in var.api_allowed_origins : origin != "*" && (startswith(origin, "https://") || startswith(origin, "http://localhost"))])
    error_message = "api_allowed_origins must contain exact HTTPS origins or localhost development origins, never a wildcard."
  }
}

variable "api_access_log_retention_days" {
  description = "CloudWatch retention for HTTP API access logs"
  type        = number
  default     = 30
}

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
