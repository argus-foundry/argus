## Decisions

- **Source is the engine's `prompt.suggest` hook**, whose `origin.kind === 'suggestion'` is the engine's own next-prompt guess. The hook calls `next(e)` first and reports only when `isShown` (the engine drops proposals while the box holds text or a turn runs). Nothing is scraped from terminal text.
- **No explicit provider clear event exists**, so clearing is derived: new turn, session/generation change, new boot, and loss of live authority (GET returns `null`). A person typing into the box (which hides the engine's suggestion) is not observable; the stale value is cleared at the next turn start. Deviation from the ticket's "explicit provider clear": the daemon honors `suggestion.cleared`, but the mod never sends it because the current engine exposes no clear trigger.
- **Loss of live authority emits no invalidation**: GET already answers `null`, but no event fires when a bridge goes silent (no daemon hook exists for that transition). Clients refetch on the existing task/session events.
- **Generation is mandatory for suggestions.** The ingest path already drops events whose generation differs from the running launch; a `suggestion.changed` with no generation is additionally dropped (legacy reports stay diagnostic only).
- **400 on invalid text covers the whole batch**, validated before any event applies, like measurements. The mod drops 4xx batches without retry.
- **Event is an invitation only** and emitted on any change of the stored suggestion (appear, replace, clear), including clears caused by other events.
- `recorded_at` is the daemon's receive time in UTC.
