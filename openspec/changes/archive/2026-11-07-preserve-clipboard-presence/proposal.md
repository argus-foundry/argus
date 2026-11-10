## Why

The `/api/tasks/{id}/stream` clipboard event derived presence from `text != ""`, so staging an empty string looked like a clear. Live events could disagree with the connect snapshot and with `GET /clipboard`.

## What changes

- `clipboard.Store.Subscribe` callbacks receive an `Update{Text, Present}`; `Set` (including `""`) is present, `Clear` and expiry are not.
- The SSE `clipboard` event emits `{"text":""}` for `Set("")` and `{"cleared":true}` only for clear or expiry; text and presence travel as one value.
- `GET`, `POST`, `DELETE` routes and status codes are unchanged.

## Capabilities

### Modified capabilities

- `clipboard-staging`: subscriber presence.
- `rest-api`: stream event shape.

## Non-Goals

- No web, TUI or macOS change. Web follow-up: ENG-1112 (Use clipboard event presence directly).

## Impact

- `internal/clipboard/store.go`, `internal/api/handlers.go`.
