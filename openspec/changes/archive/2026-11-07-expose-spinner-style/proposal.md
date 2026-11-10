## Why

The web working indicator could not follow the TUI's configured spinner style because `/api/settings` omitted it.

## What changes

- `GET /api/settings` returns `display: {"spinner_style": ...}`; `PUT` accepts an optional `display.spinner_style` (progress, dots, braille, classic).
- It reads and writes the existing `ui.spinner` config key the TUI uses; no second default.
- An unknown style returns 400 `{"error":"invalid spinner style","code":"invalid_spinner_style"}` before any section of the request is written.

## Capabilities

### Modified capabilities

- `settings-view`: display section.
- `rest-api`: settings display field.

## Non-Goals

- No web, TUI or macOS change; Scribe stays a browser-only rendering. Web follow-up: ENG-1109 (Follow the configured spinner style). Mac adoption is a named native follow-up.

## Impact

- `internal/api/settings.go`, README Reference.

Named follow-up: Remote TUI project-profile transport and spinner editing. Existing adapter gaps remain outside this change. macOS controls remain in the native-client milestone.
