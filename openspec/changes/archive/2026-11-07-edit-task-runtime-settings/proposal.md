# Edit task runtime settings safely

## Why

The workspace cannot change an existing task's model, backend, sandbox override or saved prompt without a raw overwrite that races other updates and never applies the change to the process.

## What Changes

`GET` and `PATCH /api/tasks/{id}/runtime` in `internal/api/runtime.go`; a targeted `db.SetTaskRuntime` writer; `task.runtime_changed` event; `sandbox_override` joins the `runtime` field of `task.updated`. No schema change.

## Non-Goals

Web (ENG-1076) and TUI (ENG-1091) controls are the named follow-ups; no UI changes here. macOS runtime controls: named follow-up, ticket to be filed. Swapping the minimal family-based model check for `agent.ValidateExplicitModel` (ENG-1063) is a follow-up once it merges.
