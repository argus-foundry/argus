## Why

A project's default account cannot be set outside the TUI. Linear ENG-1046.

## What changes

- `GET /api/account-defaults` returns `{global, projects}` entries of `{explicit, effective, source, editable}`.
- `PUT /api/account-defaults` and `PUT /api/projects/{name}/account` take `{"account": "<name>"|null}` (null clears).
- config.toml-owned selections are `editable:false` and answer 409. Existing tasks never move.
- Writes emit `config.changed` `{"resource":"account_defaults","project":"<name>"}` (project omitted for global).

## Capabilities

### Modified capabilities

- `rest-api`

## Non-Goals

Web UI (ENG-1087) and macOS/TUI changes are follow-ups. Resolution precedence is unchanged.
