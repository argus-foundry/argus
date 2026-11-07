## Why

A failure a standalone task reports through `task_set_result` had no first-class field, so Web could not show it like a failed Hera role.

## What Changes

- Task JSON carries `failure`: `null` or `{"reason","source":"task_result","recorded_at"}`, parsed from the documented Result `{"failed":true,"reason":"..."}`.
- `recorded_at` is persisted in a new `tasks.result_at` column whenever the result changes (never request time).
- New `task.failure_changed` event with `{"failure":null|{...}}` after a result write moves the projection.

## Non-Goals

- No Web, TUI or Mac UI change. Follow-ups: ENG-1072 (Web) and ENG-1090 (TUI).
- User stop, supervisor disconnect, nonzero exit and arbitrary Result text are never classified as failure; the raw Result stays opaque.
