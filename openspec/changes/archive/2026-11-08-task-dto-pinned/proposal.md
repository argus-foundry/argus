# Include pinned state in task responses

Task snapshots omit the stored pin flag, so clients cannot recover pin state after reconnects or missed events.

## Change

Always emit the stored boolean `pinned` in list and single task responses through the shared task DTO builder. Pin, unpin, archive and pin restore continue to use existing persistence and invalidation behavior.

## Non-Goals

No routes, events or client changes. Theia consumption ships in the Theia adoption branch. The existing SPA consumes parsed JSON objects and tolerates added keys. ArgusKit's keyed decoder ignores unknown keys, so macOS can decode the additive response; exposing pin state in its typed Task model remains the named macOS task pin consumption follow-up. The TUI already reads stored pin state directly.

## Validation

Add red-first handler tests for explicit true and false fields in both GETs, and verify stored pin/archive state after every mutation. Run focused API race tests and one full pre-pr gate.
