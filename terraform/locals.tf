locals {
  region = "us-west-2"

  context = data.context_tags.backend
  name    = data.context_label.backend.rendered


  tags = local.context.tags
}
