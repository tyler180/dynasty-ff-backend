locals {
  region = "us-west-2"

  context_tags = data.context_tags.backend
  name         = data.context_label.backend.rendered


  tags = local.context_tags.tags
}
