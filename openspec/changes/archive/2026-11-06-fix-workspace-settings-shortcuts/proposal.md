# Fix workspace shortcuts leaving Settings

## Why
Global shortcuts can open hidden or inert surfaces underneath full-window Settings.

## What Changes
Route shortcuts that open workspace surfaces through Settings leaveForTask before opening the target. Refuse departure during an in-flight save, matching Back. Preserve compact drawer dismissal and creation drafts.

## Scope
Optional web workspace only. No REST, Go, TUI or native macOS changes. Unsaved-edit confirmation does not exist at the base and remains a named follow-up.
