## Why

The decision page omits FYIs, and reaching it requires a TUI keypress. Aaron needs to handle both decisions and FYIs from his phone's authenticated PWA without choosing a coordinator first.

## What changes

- Add one shared page with a desktop left rail listing rail-visible coordinators and nested sub-coordinators in Hera rail order; select one board with Decisions/FYIs tabs and counts. Empty coordinators remain selectable but greyed out.
- Collapse the same navigation tree into a phone drawer; add desktop Cmd/Ctrl arrow shortcuts with text-field exemptions and preserve queued responses across boards.
- Let FYIs receive a comment, a silent human dismissal, or comment and dismissal together; comments use the existing durable board-notice outbox and do not resolve unless dismissal is requested.
- Add a device-authenticated launch endpoint using the existing single-use fragment-ticket exchange and a board-only session authorized against current rail-visible coordinators. Both entry points open the same page.
- Add an always-available web-app entry with an aggregate open-count badge; phone launch selects the first non-empty coordinator, while TUI `o` preselects its current coordinator, including an empty or FYI-only board.
- Extend drafts, staleness handling and successful-submit completion to all actions across both tabs and all coordinators.
- Exclude macOS, live push updates and `/decision` skill integration.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `board-decision-page`: Shared rail/tree navigation, board-specific tabs, desktop shortcuts, phone drawer, FYI actions, draft recovery and completion across submitted actions.
- `rest-api`: Shared authenticated launch/counts/navigation, current rail-visible coordinator authorization, mixed-item reads and retry-safe FYI action endpoint.
- `coordinator-board`: Durable FYI comments, silent human dismissal and atomic comment-and-dismiss notices.
- `mobile-pwa`: Direct Decide entry with an open-count badge and secure same-origin launch.
- `hera-view`: TUI `o` opens the shared page with its coordinator preselected, even when empty, and focuses selected decisions or FYIs.

## Impact

Future implementation affects the board database/outbox, API session middleware and handlers, standalone `/decide`, PWA shell and service-worker version, TUI launch guard and associated tests/docs. No external dependencies or spec tooling in CI. This branch contains specs only; Aaron must approve before implementation. The change stays active until its implementation PR archives it before merge.
