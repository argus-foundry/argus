## Domain boundaries

TaskComposer owns drafts, selection and explicit sends. ComposerSuggestion owns the hint and its authority lifetime. ComposerBridge shares one in-flight bridge read between suggestions and the existing ComposerContext. It keeps no settled cache and invalidates reads on selection, disconnect, turn and suggestion changes.

## Lifetimes

Suggestion text is accepted only from a live bridge whose task, session and generation match. The 45-second expiry follows last_event_at, matching bridge.LiveWindow. A visible empty hint checks authority every 15 seconds with the existing requestSignal deadline. Ordinary renders preserve the poll deadline, and visibility return checks expiry before awaiting a read.

Suggestion and session invalidations reach the composer before task snapshots buffer events. A 100-millisecond debounce coalesces bursts. The textarea keeps its label and receives text through the placeholder property only when its value is empty; unchanged attributes are not rewritten.
