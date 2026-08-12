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