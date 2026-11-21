# Dynasty Football Backend

This Go 1.26 repository owns provider ingestion, canonical player identity,
league snapshots, persistence, model-input assembly, and AWS deployment.
Keep analysis and draft logic in `dynasty-ff-models` and consume its public
`analysis` and `draft` packages. Use AWS SDK for Go v2 for AWS clients.

## Development checks

- Run `make gotest` for Go changes. It runs the tests, race detector, and vet.
  Run `make build` when changing a CLI or Lambda entry point.
- For Terraform changes, run `terraform fmt -check -recursive`,
  `terraform -chdir=terraform init`, and
  `terraform -chdir=terraform validate`. Check the PR plan for resource
  replacement, data loss, or permission changes.
- Build Lambda image changes with `make docker-build`. The build uses the
  parent directory as context and expects sibling checkouts of
  `dynasty-ff-models` and `mfl/mfl-mcp`.
- Use fixtures, fake clients, or local HTTP test servers for verification.
  Running live syncs or applying infrastructure requires explicit authorization.

## Pull request review

- Report actionable regressions introduced by the diff. Cite the changed lines,
  name the affected caller or workflow, and describe the observable impact.
  Prioritize security, incorrect results, data loss, and deployment failures.
  Follow [SECURITY.md](SECURITY.md) for security-sensitive changes.
- Review HTTP changes across the Terraform routes, Lambda HTTP adapter, and
  affected frontend contract. Preserve public request and response shapes,
  query defaults, status codes, and the separation between HTTP routes and
  direct Lambda actions.
- Check provider changes against older and newer payloads. Preserve
  season-specific scoring, franchise IDs with leading zeros, and canonical
  player identity. Flag partial data that silently removes players or
  overwrites previously valid data.
- For persistence and scheduled ingestion, trace retries and partial failures.
  Check DynamoDB key and index compatibility, S3 key layout, archive
  deduplication, and how existing snapshots remain readable. Verify that a
  failed refresh cannot publish incomplete data as the latest snapshot.
- When model inputs change, verify field mappings, units, and missing-data
  behavior against the public model API. Check that CI and the Lambda image
  still build against the intended dependency revisions.
