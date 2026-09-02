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
- `internal/app/snapcountsync`: PFR/nflverse defensive snap-count ingestion
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

## Authenticated HTTP API

Terraform provisions an API Gateway HTTP API in front of the same Lambda. Its
JWT authorizer accepts tokens only from the configured HTTPS issuer and audience.
Set these values for your OIDC provider when planning or applying:

```hcl
api_jwt_issuer    = "https://auth.k8s.749rmw.com/application/o/dynasty-ff/"
api_jwt_audiences = ["dynasty-ff-frontend"]
api_allowed_origins = [
  "https://dynasty-ff.749rmw.com",
  "http://localhost:3000",
]
```

Create an Authentik OAuth2/OpenID provider and application for `dynasty-ff`
before applying. The issuer must exactly match the token's `iss` claim,
including Authentik's trailing slash. Use that provider's client ID as the API
audience. The provider's redirect URI belongs to the self-hosted frontend; no
OIDC client secret is stored in this backend or sent to a browser.

Configure these strict redirect URIs on the public Authentik client before
wiring frontend sign-in:

```text
https://dynasty-ff.749rmw.com/auth/callback
http://localhost:3000/auth/callback
```

The HTTP boundary exposes only explicit application workflows. It does not expose
a generic action route, identity writes, arbitrary snapshot writes, or identity
sync. API Gateway validates the bearer token before these routes reach Lambda:

- `POST /v1/analyze`
- `POST /v1/snapshots/sync`
- `GET /v1/snapshots/latest`
- `GET /v1/snapshots/at`

`GET /health` is public for load-balancer and uptime probes and returns no league
data. The analyze request body is the direct action payload without an `action`
field:

```json
{
  "season": 2026,
  "league_id": "79286",
  "franchise_id": "0005",
  "projection_fallback": "auto",
  "cap_relief_target": 10
}
```

Call it with an OIDC access token:

```sh
curl --fail-with-body \
  --request POST "$DYNASTY_API_URL/v1/analyze" \
  --header "Authorization: Bearer $DYNASTY_ACCESS_TOKEN" \
  --header "Content-Type: application/json" \
  --data '{"season":2026,"league_id":"79286","franchise_id":"0005","projection_fallback":"auto","cap_relief_target":10}'
```

API errors intentionally omit internal storage and provider details. API Gateway
access logs contain request metadata but not bearer tokens or request bodies.

`POST /v1/snapshots/sync` is the draft-time refresh path. It queues an asynchronous
Lambda invocation and returns `202 Accepted`, avoiding API Gateway's synchronous
integration timeout. The worker always includes live draft data and skips
FantasyPros calls so availability cannot be blocked by an optional provider. The
request body contains only the league coordinates; poll `/v1/snapshots/latest`
until `observed_at` changes, then call `/v1/analyze`.
Draft-time workers overlay the fresh roster, picks, availability, and rookie ADP
onto the newest snapshot containing historical points and replacement levels, so
the fast path does not discard durable analysis inputs.

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
  "include_draft": true,
  "skip_fantasypros": true
}
```

Set `skip_fantasypros` to `true` for a zero-call fallback when FantasyPros is
unavailable or rate limited. The stored snapshot will use MFL rosters, draft
state, league-scored history, rookie ADP, and the actual free-agent pool. When
the flag is omitted, FantasyPros enrichment is attempted but provider failures
produce a warning instead of aborting the MFL sync.

## Defensive snap participation

Run `sync_identities` first so PFR aliases resolve to canonical player IDs, then
import one season of game-level snap facts from nflverse:

```json
{
  "action": "sync_snap_counts",
  "season": 2025
}
```

The sync stores defensive snaps and defensive-snap percentage for defensive
players in the versioned league-data bucket at
`snap-counts/<season>/latest.json`. Percentages are fractions from 0 through 1,
so `0.83` represents 83%. The response reports unresolved PFR player IDs rather
than guessing identity matches.

Each fact also stores the corresponding team's defensive snap total, derived
from the highest-participation player row for that game and team. Downstream
models can therefore aggregate usage as `sum(player snaps) / sum(team snaps)`
instead of averaging rounded game percentages.

Canonical players can be queried directly through the Lambda action:

```json
{
  "action": "get_snap_counts",
  "player_ids": ["player-canonical-id"],
  "seasons": [2024, 2025],
  "position_groups": ["LB"]
}
```

The authenticated API exposes the same query as:

```text
GET /v1/players/snaps?player_ids=player-canonical-id&seasons=2024,2025&position_groups=LB
```

Results remain game-level facts so a future model can calculate rolling or
weighted trends from snap totals instead of averaging percentages across games.
The source dataset is provided by the
[nflverse data project](https://github.com/nflverse/nflverse-data) under
CC-BY-4.0 and is derived from Pro Football Reference game-level snap counts.

Rank the current MFL league's defensive free agents by sustained increases in
defensive snap participation:

```json
{
  "action": "top_defensive_free_agent_trends",
  "season": 2026,
  "league_id": "79286",
  "seasons": [2023, 2024, 2025],
  "limit": 10
}
```

The action reads the live MFL free-agent pool, resolves MFL IDs through the
canonical identity table, loads the requested S3 snap seasons, and returns only
the highest-ranked `rising` signals. When `seasons` is omitted, it defaults to
the three seasons before the requested league season; `limit` defaults to 10.
The model compares a raw-snap-weighted three-game baseline with the three most
recent games. PFR's stored per-game `defense_snap_pct` is used directly in
the weekly trend and is used to confirm that at least two recent games show a
10-point or larger increase. The multi-game windows remain weighted as
`sum(defense_snaps) / sum(team_defense_snaps)` rather than averaging percentages.

The authenticated API exposes the same result:

```text
GET /v1/free-agents/defensive-trends?season=2026&league_id=79286&seasons=2023,2024,2025&limit=10
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
FantasyPros dynasty rank and its transparent rank-derived market value are also
persisted for rostered players. Drop analysis uses them to protect tradeable
assets and can recommend a multi-player cut package that meets the cap target.
Replacement options are specific players in the current MFL free-agent pool.
The sync reads the prior three seasons of successful blind-bid waivers and uses
the position-level median, 75th percentile, and 90th percentile winning bids to
report an estimated acquisition salary, competition, and confidence. Each option
shows gross and net cap relief, roster impact, BBID-budget fit, and current MFL
lock status. Pending and losing bids are not exposed by MFL and remain explicitly
outside the model.
The response contains separate offense and IDP rookie boards built only from
players currently in MFL's free-agent rookie pool. Official FantasyPros
rookie/dynasty ECR and preseason PPR projections provide the top-of-board signal.
MFL's read-only aggregate ADP from recent real rookie-only drafts matching the
league size adds a deeper market signal; the 5% selection cutoff typically reaches
about six rounds in a 12-team league.
Each board reports ranked and unranked candidate counts and retains unranked MFL
rookies instead of silently dropping them. Market values are a transparent
exponential transform of ECR for ordering; rookie ADP remains separately visible,
and offense and IDP values are not compared across boards.

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
