## Why

Decide labels comments on items closed during review as bare `invalid`, and labels cleared conflict responses as missing notes. These messages obscure which responses succeeded and what needs attention.

## What changes

- Include the closed item in rejected comment results while retaining `invalid` and receipt replay behavior.
- Explain closed-item rejections and distinguish stale response selection from actual note validation errors.
- Verify per-item acceptance and retry across coordinators with DB and browser regressions.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `rest-api`: Identify closed items in rejected board feedback results.
- `board-decision-page`: Give accurate per-item failure and validation feedback.

## Impact

DB board answer/FYI results and the standalone Decide page. No new endpoint, dependency, authentication or mutation semantics. TUI and macOS can ignore the existing optional item field; the standalone page is shared by all launch surfaces.
