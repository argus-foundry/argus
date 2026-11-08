## Why

The web workspace and future Mac app cannot show agent message history without consuming mail, and cannot persist human read/dismiss state for the coordinator board. ENG-1051 and ENG-1070 expose both over REST.

## What Changes

- `GET /api/tasks/{id}/hera-messages` — read-only, cursor-paged Hera history.
- `PUT /api/hera/roles/{role_id}/board/{item_id}/read-state` and `POST .../dismiss` — revision-checked human mutations with coded errors.
- `hera.board_changed` SSE event emitted after every committed board change (MCP, TUI and REST paths).

## Non-Goals

API-only. Web/TUI adoption is the follow-up: ENG-1100 (message history viewer), ENG-1118 (web human Inbox), ENG-1124 (TUI mark-unread). Mac adoption is a later parity follow-up. No reply endpoint.

## Impact

`internal/db/hera_board.go`, `hera_messages.go`, `internal/api/hera_board_mutate.go`, `hera_messages.go`, `routes.go`, `internal/model/event.go`.
