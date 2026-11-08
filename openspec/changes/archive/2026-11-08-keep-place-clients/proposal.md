## Why

#1158 made the size-drift kick keep the user's place and typed draft, but only in the TUI. The web app and the macOS app resize through `POST /api/tasks/{id}/resize`, which the daemon turns into the same kill + resume. Their scroll position lives in xterm.js / SwiftTerm and web typing goes through `term.onData`, so the daemon cannot see either: a web or macOS user scrolled up reading still loses their place, and a web user's unsent composer text is discarded unless the Claude mod happens to report it.

## What Changes

- `POST /resize` accepts optional `scrolled_up` and `composer_dirty` booleans and returns `deferred` (`"scrolled"`, `"draft"` or `""`). Old clients omit them and behave as before.
- The daemon's kick evaluation applies the TUI's order: draft first (mod report, then the daemon's own rendered-composer read of the session tail, then the client's `composer_dirty` flag, the flag under the same 10-minute per-width cap), then scroll. The rendered-composer read runs only while a kick is being evaluated, never per poll or per non-evaluating resize. `ui.rerender_keep_place` off ignores both fields.
- Web SPA: reports `scrolled_up` (from `termIsAtBottom()`) and `composer_dirty` (a JS port of the typed-since-submit fold over `term.onData`); when told `deferred`, re-POSTs the current size on scroll-to-bottom and every 10 s until the deferral clears, stopping when the terminal view goes away. `SW_VERSION` bumped.
- macOS app: the same two fields from SwiftTerm's scroll position and a Swift port of the typed fold on the send path, with the same retry and stop conditions.
- uxlog line for every daemon-side deferral and cap release.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `rest-api`: the resize request/response additions and the daemon gate order.
- `desktop-web-workspace`: the web terminal's scroll/draft reporting and deferral retry.
- `macos-app`: the same for the native terminal.

## Impact

- `internal/api/handlers.go` (+ small `Server` deferral-clock state), `internal/api/static/index.html` + `sw.js`, `macos/Sources/ArgusKit` (client, pure typed-fold), `macos/Sources/Argus/TerminalController.swift`.
- REST-only and additive: no `ProtocolVersion` or spawn-surface change, no mod change.
- **Frontend parity.** This change IS the parity work for #1158's named follow-ups; the TUI already ships it.
