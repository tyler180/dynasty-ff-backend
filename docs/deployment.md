# Deployment

Merging a PR to `main` applies the Terraform plan saved by that PR's check run,
and the Lambda image is built from the PR's head commit, not the merge commit.
A PR whose branch is behind `main` therefore deploys without the commits it is
missing, rolling back anything merged before it. Update the branch from `main`
before merging.

The check run only produces a plan when the PR touches `terraform/**` or an
image path (`cmd/**`, `config/**`, `data/**`, `docs/**`, `internal/**`,
`go.mod`, `go.sum`, `Dockerfile`; see `.github/workflows/pr.yml`). A PR that
changes nothing else, such as a README-only change, has no plan, and its merge
run fails without deploying.
