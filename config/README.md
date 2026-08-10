# Local sync inputs

`league-<MFL league ID>.json` files contain only policies MFL does not expose,
such as rookie salaries and IR/taxi cap multipliers. The live command loads the
matching file automatically.

Projection files used by `dynasty -refresh-mfl` or `dynasty-sync` can also be
placed here. Projection values must be league-scored season points keyed by the
string MFL player ID. See the repository README for accepted JSON formats.

Do not store MFL API keys or user cookies here. Supply `MFL_API_KEY` or
`MFL_USER_COOKIE` to the `mfl-mcp` subprocess through the environment.
