# Dynasty Football Backend

The application and data-integration layer for dynasty-football analysis. This
repository owns provider ingestion, canonical player identity, persistence,
league snapshots, model-input assembly, and deployment infrastructure. Pure
analysis remains in `dynasty-ff-models` and is consumed through its public
`analysis` and `draft` packages.

## Repository boundary

- `cmd/analyze`: local analysis and optimizer CLI
- `cmd/ingest-mfl`: read-only MFL snapshot ingestion
- `cmd/lambda`: AWS Lambda persistence handler
- `internal/provider/mfl`: MFL MCP adapter and normalization
- `internal/app/snapshotanalysis`: stored-fact adapter for the public analysis API
- `internal/identity`: canonical player identity contracts
- `internal/storage`: persistence adapters
- `internal/draftadapter`: conversion into the model's public input API
- `infrastructure/terraform`: backend deployment resources

## Local development

Go 1.26 or newer is expected. For simultaneous local changes across both
repositories, use a temporary Go workspace containing the sibling checkouts.

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
MFL, GSIS, or other provider IDs. When the source assigns one provider alias to
multiple MFL players, that alias is omitted and listed in `ambiguous_aliases`;
the remaining identities continue importing. Conflicts with an existing,
unambiguous mapping still stop the run for review and are never silently
replaced. The response reports source, MFL, eligible, unmatched, existing,
created, profile-write, and alias-write counts. MFL IDs missing from both the
crosswalk and DynamoDB are returned for manual review.

The Lambda timeout is 15 minutes. The importer reserves 20 seconds before its
deadline, stops scheduling new DynamoDB writes, and returns `status: "partial"`
with `identity_sync.complete: false` if more work remains. Invoke the identical
`sync_identities` request again to continue. Existing aliases are skipped and
new canonical IDs are deterministic, so retries are safe. A normal completed
response has `status: "stored"` and `identity_sync.complete: true`.

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

Analyze the latest stored snapshot by joining its canonical roster IDs to the
player profiles in DynamoDB:

```json
{
  "action": "analyze",
  "season": 2026,
  "league_id": "79286",
  "franchise_id": "0005",
  "projection_fallback": "auto",
  "cap_relief_target": 10
}
```

`projection_fallback` accepts `auto`, `historical`, or `none`. `auto` uses the
persisted MFL league-scored history unless explicit projections cover the full
roster, preventing a partial provider response from silently excluding players. The
response includes the exact snapshot timestamp used, cap and roster compliance,
draft-pick fit, taxi eligibility, and replacement-aware drop classifications.
The response also contains separate offense and IDP rookie boards built only
from players currently in MFL's free-agent rookie pool. Official FantasyPros rookie/dynasty ECR and
preseason PPR projections are fetched for offensive and IDP positions, resolved
through canonical identities, and persisted. Each board restarts its model rank
at one and reports ranked and unranked candidate counts. Unranked MFL rookies
remain visible instead of being silently dropped. Market values are a transparent
exponential transform of ECR for ordering within a board; offense and IDP values
must not be compared until IDP scarcity is calibrated to the league's scoring.

The Lambda reads both provider credentials from the configured Secrets Manager
secret:

```json
{
  "api_key": "read-only-owner-api-key",
  "fantasypros_api_key": "personal-fantasypros-api-key"
}
```

For MFL, `user_cookie` can replace `api_key`; the owner API key is preferable
because it cannot authorize MFL imports. FantasyPros data use is subject to the
API account's license and must remain personal/non-commercial unless a different
license is obtained. Scheduled sync is disabled by default; set
`mfl_sync_schedule_expression` only after the identities and credentials are
ready.
