module "dynasty_ff_backend_apigateway" {
  source  = "terraform-aws-modules/apigateway-v2/aws"
  version = "6.1.0"

  name               = "${local.name}-http"
  description        = "Authenticated dynasty football analysis API"
  protocol_type      = "HTTP"
  create_domain_name = false

  cors_configuration = {
    allow_credentials = true
    allow_headers     = ["authorization", "content-type"]
    allow_methods     = ["GET", "POST", "OPTIONS"]
    allow_origins     = var.api_allowed_origins
    max_age           = 3600
  }

  authorizers = {
    jwt = {
      authorizer_type  = "JWT"
      identity_sources = ["$request.header.Authorization"]
      name             = "${local.name}-jwt"
      jwt_configuration = {
        audience = var.api_jwt_audiences
        issuer   = var.api_jwt_issuer
      }
    }
  }

  routes = {
    "GET /v1/snapshots/at" = {
      authorization_type   = "JWT"
      authorizer_key       = "jwt"
      authorization_scopes = var.api_jwt_required_scopes

      integration = {
        uri                    = module.ff_backend_lambda.lambda_function_arn
        payload_format_version = "2.0"
        method                 = "POST"
        type                   = "AWS_PROXY"
        timeout_milliseconds   = 30000
      }
    }

    "GET /v1/snapshots/latest" = {
      authorization_type   = "JWT"
      authorizer_key       = "jwt"
      authorization_scopes = var.api_jwt_required_scopes

      integration = {
        uri                    = module.ff_backend_lambda.lambda_function_arn
        payload_format_version = "2.0"
        method                 = "POST"
        type                   = "AWS_PROXY"
        timeout_milliseconds   = 30000
      }
    }

    "POST /v1/analyze" = {
      authorization_type   = "JWT"
      authorizer_key       = "jwt"
      authorization_scopes = var.api_jwt_required_scopes

      integration = {
        uri                    = module.ff_backend_lambda.lambda_function_arn
        payload_format_version = "2.0"
        method                 = "POST"
        type                   = "AWS_PROXY"
        timeout_milliseconds   = 30000
      }
    }

    "POST /v1/snapshots/sync" = {
      authorization_type   = "JWT"
      authorizer_key       = "jwt"
      authorization_scopes = var.api_jwt_required_scopes

      integration = {
        uri                    = module.ff_backend_lambda.lambda_function_arn
        payload_format_version = "2.0"
        method                 = "POST"
        type                   = "AWS_PROXY"
        timeout_milliseconds   = 30000
      }
    }

    "GET /health" = {
      authorization_type = "NONE"

      integration = {
        uri                    = module.ff_backend_lambda.lambda_function_arn
        payload_format_version = "2.0"
        method                 = "POST"
        type                   = "AWS_PROXY"
        timeout_milliseconds   = 30000
      }
    }
  }

  stage_access_log_settings = {
    create_log_group            = true
    log_group_retention_in_days = var.api_access_log_retention_days
    format = jsonencode({
      context = {
        request_id         = "$context.requestId"
        request_time       = "$context.requestTime"
        http_method        = "$context.httpMethod"
        route_key          = "$context.routeKey"
        status             = "$context.status"
        response_length    = "$context.responseLength"
        integration_status = "$context.integrationStatus"
        authorizer_error   = "$context.authorizer.error"
        source_ip          = "$context.identity.sourceIp"
      }
    })
  }

  tags = data.context_tags.backend.tags
}

# data "aws_apigatewayv2_apis" "backend" {
#   name = "${local.name}-http"
# }

# data "aws_apigatewayv2_api" "backend" {
#   api_id = one(data.aws_apigatewayv2_apis.backend.ids)
# }

resource "aws_lambda_permission" "http_api" {
  statement_id  = "AllowExecutionFromHTTPAPI"
  action        = "lambda:InvokeFunction"
  function_name = module.ff_backend_lambda.lambda_function_name
  principal     = "apigateway.amazonaws.com"
  source_arn    = "${module.dynasty_ff_backend_apigateway.api_execution_arn}/*/*"
}
