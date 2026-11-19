## Why

The decision page omits FYIs, and reaching it requires a TUI keypress. Aaron needs to handle both decisions and FYIs from his phone's authenticated PWA without choosing a coordinator first.

## What changes

- Add Decisions and FYIs tabs with open counts, grouped by coordinator for a phone launch.
- Let FYIs receive a comment, a silent human dismissal, or comment and dismissal together; comments use the existing durable board-notice outbox and do not resolve unless dismissal is requested.
- Add a device-authenticated aggregate launch endpoint using the existing single-use fragment-ticket exchange, with sessions bounded to an explicit coordinator-role set.
- Add an always-available web-app entry with an aggregate open-count badge; keep the TUI `o` path working, including boards containing only FYIs.
- Extend drafts, staleness handling and successful-submit completion to all actions across both tabs and all coordinators.
- Exclude macOS, live push updates and `/decision` skill integration.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `board-decision-page`: Two item tabs, coordinator grouping, FYI actions, draft recovery and completion across submitted actions.
- `rest-api`: Aggregate authenticated launch/counts, bounded session scope, mixed-item reads and retry-safe FYI action endpoint.
- `coordinator-board`: Durable FYI comments, silent human dismissal and atomic comment-and-dismiss notices.
- `mobile-pwa`: Direct Decide entry with an open-count badge and secure same-origin launch.
- `hera-view`: TUI `o` launches when either decisions or FYIs are open and focuses FYIs.

## Impact

Future implementation affects the board database/outbox, API session middleware and handlers, standalone `/decide`, PWA shell and service-worker version, TUI launch guard and associated tests/docs. No external dependencies or spec tooling in CI. This branch contains specs only; Aaron must approve before implementation. The change stays active until its implementation PR archives it before merge.
