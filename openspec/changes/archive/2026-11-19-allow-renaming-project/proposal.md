## Why

A project's name is immutable once created: the TUI edit form shows it read-only
and the REST `PUT /api/projects/{name}` only updates fields. Fixing a typo or
adopting a new naming scheme means delete + re-add, which orphans every task,
schedule and per-project account mapping that references the old name.

## What Changes

- New `db.RenameProject(old, new)`: one transaction that renames the `projects`
  row and rewrites every reference — `tasks.project`, `scheduled_tasks.project`,
  `kb_pending_tasks.project`, `sandbox_proposals.project`,
  `hera_roles.argus_project`, and the key in the `accounts.projects` config JSON.
- Rejected: empty/invalid new name, new name already exists (409), old name
  missing (404), project defined in `config.toml` (409, same rule as update).
- REST: `PUT /api/projects/{name}` accepts an optional `new_name`; response
  carries the project under its new name. Emits `config.changed` for both names.
- TUI: the project edit form's name field becomes editable; saving a changed
  name routes through rename. Remote mode goes through the same REST call.
- Existing worktrees are NOT moved: tasks keep their stored worktree path
  (`~/.argus/worktrees/<oldname>/...`); only newly created tasks use the new
  directory name.

## Non-Goals / Named Follow-ups

- Web SPA and macOS app edit-form name field: follow-up (REST surface lands
  here, so both can adopt it; the macOS Settings project editor is read-only
  for name today).
- Moving worktree directories on disk.

## Capabilities

### Modified Capabilities

- `rest-api`: `PUT /api/projects/{name}` supports `new_name`.
- `settings-view`: project form name is editable on edit.
