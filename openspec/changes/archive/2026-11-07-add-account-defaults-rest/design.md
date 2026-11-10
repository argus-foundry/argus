## Decisions

- Any authenticated token may write defaults (existing settings authority); account CRUD stays master-only.
- `source` values: `database`, `config_file`, `inherited` (project with no explicit selection) and `builtin` (global with none).
- `effective` comes from `Config.ResolveAccount`, so a stored selection naming a missing account stays visible as `explicit` while `effective` falls through.
- `"default"` is accepted as an explicit selection (it differs from clearing). The tier step in the resolution chain is not part of this endpoint (routing resolves per spawn).
