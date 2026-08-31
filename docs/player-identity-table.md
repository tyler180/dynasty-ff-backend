# Player identity table

The production player identity repository uses one DynamoDB table with a
composite primary key:

| Attribute | Type | Key |
| --- | --- | --- |
| `pk` | String | Partition key |
| `sk` | String | Sort key |

No secondary index is required for the initial access patterns.

## Items

Canonical player profile:

```text
pk = PLAYER#<canonical-player-id>
sk = PROFILE
entity_type = player_profile
```

Provider alias:

```text
pk = ALIAS#<PROVIDER>#<external-player-id>
sk = PROFILE
entity_type = player_alias
player_id = <canonical-player-id>
```

For example, resolving an MFL player requires one strongly consistent read of
`ALIAS#MFL#15751` followed by one strongly consistent read of the canonical
`PLAYER#...` profile.

Automatic ingestion cannot silently remap an alias to another canonical
player. A conflicting conditional write returns `identity.ErrAliasConflict`
for review. An explicitly marked manual override can replace the mapping.

## Lambda configuration

Terraform should provide the table name to the Lambda function as a
non-secret environment variable:

```text
PLAYER_IDENTITY_TABLE=<table-name>
```

The Lambda execution role needs these actions on the table:

```text
dynamodb:GetItem
dynamodb:PutItem
```

`sync_identities` and the nflverse snap importer use `dynamodb:BatchGetItem` to
resolve provider aliases efficiently. Transactional permissions are not needed.

PFR aliases imported by `sync_identities` are also used to resolve nflverse
snap-count rows to canonical player IDs. Run identity sync before
`sync_snap_counts`; unresolved PFR IDs are reported and are not matched by name.

The table should use point-in-time recovery and encryption at rest. On-demand
capacity is a reasonable initial setting for manually triggered, low-volume
ingestion and analysis.
