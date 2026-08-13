# Dynasty Football Backend

The application and data-integration layer for dynasty-football analysis. This
repository owns provider ingestion, canonical player identity, persistence,
league snapshots, roster analysis, and deployment infrastructure. The
deterministic optimizer remains in `dynasty-ff-models` and is consumed
through its public `draft` package.

## Repository boundary

- `cmd/analyze`: local analysis and optimizer CLI
- `cmd/ingest-mfl`: read-only MFL snapshot ingestion
- `cmd/lambda`: AWS Lambda persistence handler
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

## Lambda persistence API

The Lambda reads `PLAYER_IDENTITY_TABLE` and `LEAGUE_DATA_BUCKET` from its
environment. Terraform supplies both values from the resources it manages.
Invoke it with an explicit action. For example, the health check is:

```json
{"action":"health"}
```

Normalized league snapshots support `put_snapshot`, `latest_snapshot`, and
`snapshot_at`. Snapshot objects are immutable and stored below:

```text
snapshots/<season>/<league-id>/<franchise-id>/<UTC timestamp>.json
```

Canonical identity bootstrap and lookup support `put_player`, `put_alias`,
`put_identities`, `get_player`, and `resolve_player`. `put_identities` accepts
`players` and `aliases` arrays for crosswalk bootstrap, writing all profiles
before their aliases. A player profile must be written before an alias that
points to it. Example requests:

```json
{
  "action": "put_player",
  "player": {"id": "canonical-player-id", "display_name": "Player Name"}
}
```

Normal operation uses `sync_identities`, not manual batches. It fetches the
current league's rostered and free-agent MFL IDs through the bundled read-only
MCP server, joins those IDs to the weekly DynastyProcess provider crosswalk,
and idempotently writes canonical profiles and provider aliases:

```json
{
  "action": "sync_identities",
  "season": 2026,
  "league_id": "79286"
}
```

The importer uses batch-consistent reads to preserve existing identities and
manual mappings. New canonical IDs are deterministic opaque UUIDs rather than
MFL, GSIS, or other provider IDs. Conflicting cross-provider mappings stop the
run for review; they are never silently replaced. The response reports source,
MFL, eligible, unmatched, existing, created, profile-write, and alias-write
counts. MFL IDs missing from both the crosswalk and DynamoDB are returned for
manual review.

The crosswalk is fetched at runtime from the public
[DynastyProcess data repository](https://github.com/DynastyProcess/data), which
is updated weekly and licensed GPL-3.0. Override `IDENTITY_SOURCE_URL` only for
a controlled mirror or test fixture.

```json
{
  "action": "put_alias",
  "alias": {
    "external_id": {"provider": "mfl", "value": "15751"},
    "player_id": "canonical-player-id",
    "source": "manual-bootstrap",
    "resolution_method": "manual",
    "manual_override": true,
    "ingested_at": "2026-08-13T00:00:00Z"
  }
}
```

After `sync_identities` completes, `sync_mfl` runs the bundled
read-only `mfl-mcp`, resolves every roster player through the identity table,
and writes the normalized snapshot to S3:

```json
{
  "action": "sync_mfl",
  "season": 2026,
  "league_id": "79286",
  "franchise_id": "0005",
  "include_draft": true
}
```

The Lambda reads its MFL credential from the configured Secrets Manager secret.
The supported minimal secret shape is:

```json
{"api_key":"read-only-owner-api-key"}
```

Alternatively, use `{"user_cookie":"MFL_USER_ID cookie value"}`. The owner API
key is preferable because it cannot authorize MFL imports. Scheduled sync is
disabled by default; set `mfl_sync_schedule_expression` only after the identity
aliases and credential secret are ready.
