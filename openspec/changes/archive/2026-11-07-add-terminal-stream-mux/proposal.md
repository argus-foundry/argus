## Why

Keeping several visited task terminals live over the web workspace needs one `EventSource` per task, and hidden streams are suspended and later re-caught-up from bounded retained output. A single multiplexed connection removes the per-task connection cost and gives clients explicit resync signals.

## What Changes

- Add `POST /api/terminal-streams`, `GET /api/terminal-streams/{id}/events`, `PUT /api/terminal-streams/{id}`, `DELETE /api/terminal-streams/{id}`.
- One SSE connection multiplexes up to 64 task subscriptions with `output`, `gap`, `reset`, `exit` and `subscription_error` events carrying exact byte cursors.
- Handles bind to the creating principal, expire after 60 seconds without a connection, and release every session writer on close.

## Non-Goals

- No web-workspace, static SPA, TUI or macOS change. Web consumption is the follow-up ENG-1086 (Web: Keep visited terminals current in the background).
- The existing per-task `GET /api/tasks/{id}/stream` is unchanged.
