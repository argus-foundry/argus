# Show measured current session totals and paginated turns alongside measured Codex context.

## Scope

ENG-1080, ENG-1078. Frontend only, local commits.

## Non-Goals

No daemon, SPA or macOS changes. Named follow-ups are SPA adoption of theia-session-usage and macOS adoption of theia-session-usage; existing TUI behavior remains unchanged. No live daemon verification.

## Abstraction inventory

ComposerContext owns context authority. SessionUsage owns current session totals, turn paging and cancellation. Usage views render counters without combining sessions or historical bindings.
