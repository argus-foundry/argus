## Why

Aaron requested a remembered appearance choice and clickable references on the decision page. These updates are pre-approved and make reading and following decision context easier on phones.

## What changes

- Add a visible Light / Dark / System selector, defaulting to System, restored before first paint from guarded localStorage access.
- Link HTTP(S) references in decision text using DOM nodes, preserving punctuation, paragraphs and code spans.
- Preserve same-origin page requests, fragment stripping and credential storage boundaries.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `board-decision-page`: Remember appearance and safely link decision references.

## Impact

The standalone `/decide` shell, service-worker version, page-logic tests and browser verification. No REST changes or new dependencies; TUI and macOS clients are unaffected.
