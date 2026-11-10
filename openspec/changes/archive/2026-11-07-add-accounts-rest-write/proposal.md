## Why

Web users can inspect accounts but not add, edit or remove them; only the host TUI can. Linear ENG-1034.

## What changes

- `GET /api/accounts` entries gain `source` (`database` / `config_file` / `builtin` per field) and `editable`.
- `POST /api/accounts`, `PATCH /api/accounts/{name}`, `DELETE /api/accounts/{name}`: master-token only, validated with the shared account validators, 8 KiB body cap, unknown fields rejected.
- Delete is refused with 409 and a `references` body while tasks, the global default, project mappings or routing tiers name the account.
- A committed write emits `config.changed` `{"resource":"accounts","name":"<name>"}`.

## Capabilities

### Modified capabilities

- `rest-api`

## Non-Goals

Web UI (ENG-1075) and TUI/macOS changes are follow-ups. Sign-in/login flows are out of scope; directory edits only register references.
