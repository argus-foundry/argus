# Fence retired supervisor exits

## Why

The daemon can handle a supervisor exit after a newer session for the same task has started. Task-scoped exit effects can otherwise change the replacement's status, transcript, or streams.

## What Changes

Scope the supervisor's exit record and the daemon's finalization to the outgoing session. Coordinate the finalization with task stop and resume. Clients on the TUI, web, and macOS continue to receive the owning daemon's lifecycle events.

Bind resize and terminal streams to the identified process. Persist connected Hera reclaim intent for the outgoing process so its exit can complete the archived task.
