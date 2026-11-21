# The image is built here rather than through the terraform-aws-modules
# docker-build module because that module cannot disable BuildKit attestations.
# With attestations enabled, newer Docker engines push an OCI image index that
# carries a provenance manifest, and Lambda rejects that image with
# "The image manifest, config or layer media type ... is not supported".

resource "aws_ecr_repository" "backend" {
  name                 = local.name
  image_tag_mutability = "MUTABLE"
  force_delete         = true

  image_scanning_configuration {
    scan_on_push = true
  }
}

resource "aws_ecr_lifecycle_policy" "backend" {
  repository = aws_ecr_repository.backend.name
  policy = jsonencode({
    "rules" : [
      {
        "rulePriority" : 1,
        "description" : "Keep only the last 5 images",
        "selection" : {
          "tagStatus" : "any",
          "countType" : "imageCountMoreThan",
          "countNumber" : 5
        },
        "action" : {
          "type" : "expire"
        }
      }
    ]
  })
}

resource "docker_image" "backend" {
  name = aws_ecr_repository.backend.repository_url

  build {
    context    = local.source_path
    dockerfile = "${local.source_path}/dynasty-ff-backend/Dockerfile"
    platform   = "linux/amd64"
    # Push a single-platform image manifest that Lambda accepts.
    provenance = "false"
    sbom       = "false"
  }

  triggers = {
    dir_sha = local.dir_sha
  }
}

resource "docker_registry_image" "backend" {
  name = docker_image.backend.name

  triggers = {
    dir_sha = local.dir_sha
  }
}

locals {
  # Deploy by digest so every build produces a new function version.
  backend_image_uri = format("%v@%v", docker_registry_image.backend.name, docker_registry_image.backend.id)
}

moved {
  from = module.ff_backend_build.aws_ecr_repository.this[0]
  to   = aws_ecr_repository.backend
}

moved {
  from = module.ff_backend_build.aws_ecr_lifecycle_policy.this[0]
  to   = aws_ecr_lifecycle_policy.backend
}

moved {
  from = module.ff_backend_build.docker_image.this
  to   = docker_image.backend
}

moved {
  from = module.ff_backend_build.docker_registry_image.this
  to   = docker_registry_image.backend
}
