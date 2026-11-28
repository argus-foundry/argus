# Settle nuke and terminal admission

## Why

A nuke owns its task once the final role transition commits, including before archival. Terminal recovery can also lose its view or encounter a stalled renderer or refused stream after a snapshot.

## What Changes

- Keep nuke completion in internal persisted state and recheck ownership under the task lock.
- Report role contention and preserve the reachable parent until its roles settle.
- Invalidate PWA attachment across all detail exits. Keep its deadline through rendering and SSE acceptance.
