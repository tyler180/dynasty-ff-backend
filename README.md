# Dynasty Football Backend

The application and data-integration layer for dynasty-football analysis. This
repository owns provider ingestion, canonical player identity, persistence,
league snapshots, roster analysis, and deployment infrastructure. The
deterministic optimizer remains in `dynasty-ff-draft-model` and is consumed
through its public `draft` package.

## Repository boundary

- `cmd/analyze`: local analysis and optimizer CLI
- `cmd/ingest-mfl`: read-only MFL snapshot ingestion
- `internal/provider/mfl`: MFL MCP adapter and normalization
- `internal/analysis/source`: roster, cap, taxi, and cut analysis
- `internal/identity`: canonical player identity contracts
- `internal/storage`: persistence adapters
- `internal/draftadapter`: conversion into the model's public input API
- `infrastructure/terraform`: backend deployment resources

## Local development

Go 1.26 or newer is expected. The local `replace` directive points at the
sibling model checkout.

```sh
make test
make build
```

Until the model is versioned remotely, Docker needs the parent directory as
its build context so it can see both sibling modules. From this repository,
run:

```sh
make docker-build
```

The equivalent direct command is:

```sh
docker build -f Dockerfile -t dynasty-ff-backend:local ..
```

Analyze the verified local league snapshot:

```sh
go run ./cmd/analyze -source data/team-mclean-2026-source.json
```

For a live refresh, build the read-only `mfl-mcp` server and provide its path
and league identity through environment variables:

```sh
export MFL_MCP_COMMAND='/path/to/mfl-mcp'
export MFL_YEAR=2026
export MFL_LEAGUE_ID=79286
export MFL_FRANCHISE_ID=0005
go run ./cmd/analyze -refresh-mfl
```

Credentials stay in the environment inherited by the MCP subprocess. Do not
put MFL credentials in command arguments, source files, or Terraform values.
