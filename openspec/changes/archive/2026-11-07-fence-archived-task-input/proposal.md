# Fence archived-task input in REST

## Why

An archived task with a live session still accepted REST input, so a stale client could type into it after archival.

## What Changes

REST handlers in `internal/api/handlers.go` gain the guards below; no schema change.

## Non-Goals

Web handling of `task_archived` (ENG-1108), the local TUI input fence (ENG-1122) and native macOS `task_archived` presentation (`macos-task-archived-presentation`) are the named follow-ups.
