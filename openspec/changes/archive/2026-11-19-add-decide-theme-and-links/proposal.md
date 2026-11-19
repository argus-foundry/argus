## Why

Aaron requested a remembered appearance choice, clickable references and closing the page after accepted submissions. These updates are pre-approved and make reading, following context and finishing decisions easier on phones.

## What changes

- Add a visible Light / Dark / System selector, defaulting to System, restored before first paint from guarded localStorage access.
- Link HTTP(S) references in decision text using DOM nodes, preserving punctuation, paragraphs and code spans.
- Close the tab after a fully successful submission, with a visible sent confirmation if the browser keeps it open; keep unsuccessful submissions editable.
- Preserve same-origin page requests, fragment stripping and credential storage boundaries.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `board-decision-page`: Remember appearance and safely link decision references.

## Impact

The standalone `/decide` shell, service-worker version, page-logic tests and browser verification. No REST changes or new dependencies; TUI and macOS clients are unaffected.
