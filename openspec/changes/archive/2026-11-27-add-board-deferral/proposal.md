## Why

Old board items crowd out new attention when Aaron wants to say “not now.” A reversible human deferral keeps those items available without repeatedly surfacing them.

## What changes

- Add a human-owned `deferred_at` timestamp to every board kind, reversible through REST only.
- Hide deferred items by default in TUI and web boards; show a toggle and deferred count.
- Suppress deferred attention, unread counts and needs-input rollups while retaining unread revisions and ordinary resolution behavior.
- Rename non-blocking action presentation from “deferred” to “later.”
- Keep coordinator list output aware of deferral and evaluate read-only macOS board visibility.

## Capabilities

### New capabilities

### Modified capabilities

- `coordinator-board`: Human deferral, attention suppression and client visibility controls.

## Impact

SQLite board schema, board REST mutations, attention summaries, TUI keymap/help, web board and Decide views, service-worker cache, macOS board rendering if present. No coordinator MCP mutation is added.
