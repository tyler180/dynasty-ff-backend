# Player-game statistics table

The player-game DynamoDB table is the query-optimized store for normalized
nflverse observations. Original source files are archived immutably at
`source-data/nflverse-snap-counts/<season>/<sha256>.csv`; the versioned normalized
S3 object remains available at `snap-counts/<season>/latest.json`.

## Player-game items

Each snap-count observation uses:

```text
pk     = PLAYER#<canonical-player-id>
sk     = GAME#<season>#<zero-padded-week>#<game-id>
gsi1pk = SEASON#<season>
gsi1sk = PLAYER#<canonical-player-id>#<zero-padded-week>#<game-id>
```

The import retains every resolved row and source column from the nflverse/PFR
snap-count feed: source player/game IDs, player name, game dimensions, offense,
defense, and special-teams snap counts and percentages, plus the derived team
defensive snap total. Provider IDs are resolved before persistence, so models
query canonical player IDs and remain independent of nflverse and AWS.

The primary key supports chronological player-history queries. The
`season-index` GSI supplies the keys needed to reconcile a refreshed season,
including deletion of rows removed by upstream corrections.

Future nflverse metrics should be added to this player-game item or stored as a
dataset-specific companion item when they have a different grain. New model
inputs should continue to depend on backend history contracts rather than AWS
or provider schemas.

Comprehensive weekly player stats use a companion item so refreshing one
dataset cannot overwrite another:

```text
pk     = PLAYER#<canonical-player-id>
sk     = GAME#<season>#<zero-padded-week>#<game-id>#PLAYER_STATS
gsi1pk = PLAYER_STATS#SEASON#<season>
gsi1sk = PLAYER#<canonical-player-id>#<zero-padded-week>#<game-id>
```

Stable dimensions are top-level attributes. Every other numeric CSV column is
stored in the `metrics` map and every non-numeric column in `attributes`, so
nflverse can add fields without requiring a DynamoDB schema migration.

## Dataset state

One metadata item is stored per imported season:

```text
pk = DATASET#SNAP_COUNTS
sk = SEASON#<season>
```

It records both the original source SHA-256 and normalized SHA-256, along with
the record count and successful import time. A scheduled sync compares these
versions before writing. Dataset state is updated only after the immutable raw
archive, normalized S3 object, and DynamoDB index have succeeded.

## Migration

After deploying the table, invoke `sync_snap_counts` once for every historical
season that should be indexed. Until then, reads for entirely unmigrated
seasons fall back to S3.

```json
{"action":"sync_snap_counts","season":2023}
```

Repeat for 2024 and 2025. A second invocation with unchanged nflverse data
returns `unchanged: true` and `stored_records: 0`.

Backfill the comprehensive weekly statistics with the parallel action:

```json
{"action":"sync_player_stats","season":2023}
```

Repeat for 2024 and 2025. The daily EventBridge rule invokes both dataset syncs
for `nflverse_sync_year`.
