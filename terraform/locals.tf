locals {
  region = "us-west-2"

  context = data.context_tags.backend
  name    = data.context_label.backend.rendered

  source_path   = path.module
  path_include  = ["**"]
  path_exclude  = ["**/terraform/**"]
  files_include = setunion([for f in local.path_include : fileset(local.source_path, f)]...)
  files_exclude = setunion([for f in local.path_exclude : fileset(local.source_path, f)]...)
  files         = sort(setsubtract(local.files_include, local.files_exclude))

  dir_sha = sha1(join("", [for f in local.files : filesha1("${local.source_path}/${f}")]))

  tags = {
    Environment = "dev"
    Project     = "mfl-backend"
  }
}
