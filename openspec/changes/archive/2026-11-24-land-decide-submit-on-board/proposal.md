## Why

Submitting Decide currently replaces the selected board with a noisy global queue. Aaron wants a calm confirmation in place, with failures and written drafts recoverable where they belong.

## What changes

- Keep selection, show scoped submit confirmation and refresh counts.
- Put failures on their cards and recover closed written drafts in collapsed disclosures.
- Tidy explicit queue review/footer and prevent harmless work readiness changes invalidating Done.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `board-decision-page`: Submit landing, failure recovery, explicit review and authored revision stability.

- `coordinator-board`: Readiness-only edits preserve authored response revisions and normal attention events.

## Impact

Standalone Decide HTML, service worker, board DB revision checks and browser/Go tests. No endpoint changes.
