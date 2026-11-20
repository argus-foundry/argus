## Context

The base board-decision-page spec and archived `add-board-decision-page` and `add-decide-theme-and-links` changes define a standalone `/decide` shell, a 90-second single-use fragment ticket, an eight-hour in-memory role-scoped session, durable answer summaries, safe DOM links, Light/Dark/System appearance and close-on-success behavior. FYIs are currently excluded and a phone user cannot initiate this flow from the PWA.

The PWA already has a per-device token. It must use that credential only in authenticated API headers, keeping the standalone page on a narrower board-only session. Aaron has selected the rail-style page described below. The coordinator’s six interaction decisions are recorded here. These spec/mockup revisions do not authorize implementation.

## Goals / non-goals

**Goals:** Handle open decisions and FYIs on a phone, retain coordinator ownership, make comment retries safe, preserve the existing TUI ticket launch mechanism and give every submitted action a verifiable result.

**Non-goals:** macOS app changes or a macOS follow-up (explicitly dropped by Aaron); live push/SSE updates (retain polling); `/decision` skill posting to the board (separate later change); editing coordinator-authored content; changing resident-approval protections; duplicating kanban section headings/collapse controls in the page (use the canonical rail order and nesting, including the ordering induced by kanban groups, without reproducing their chrome).

## Decisions

### Launch and session boundary

Both entry points mint the same board-only session via a 90-second single-use hashed fragment ticket. `POST /api/hera/board/decision-link` (master/device only) opens `/decide` for the phone; `POST /api/hera/roles/{role_id}/board/decision-link` uses `/decide?role=<id>[&focus=<item_id>]` for TUI preselection. The requested role is initial selection, not authorization scope. Empty boards may launch. An unavailable/hidden/archived target is rejected rather than exposing a board omitted from the rail.

To support one shared page whose nav matches the rail, replace the previously proposed immutable one-role/set scope with an explicit **rail-board session class**: eight-hour absolute lifetime, 256-bit random credential hashed only in daemon memory, authorized only for board reads/answers/FYI actions and the new scoped navigation endpoint. Every request resolves coordinator membership through the canonical Hera rail visibility policy: exclude archived, hidden and nuked coordinators/orchestrators and plain workers; include nested sub-coordinators and coordinators without open items or live bindings. New visible coordinators can appear on accepted refresh; hidden/removed coordinators immediately cease to be authorized. This dynamic scope is deliberate and limited to the human’s current visible boards, never a device-equivalent API credential.

Exchange returns `{session,initial_role_id,expires_at}`. `GET /api/hera/board/decision-navigation` supplies the currently permitted coordinator tree, in rail order, with parent role IDs and kind counts. The page only sends its credential to that route and the allowed per-role decision/answer/mixed-item/FYI routes. It cannot mint links, access broad discovery/roster endpoints, mutate general board metadata or read privileged receipt history. Count discovery remains master/device-only for the PWA badge. Requests that lose visibility fail closed; queued drafts remain recoverable but are not submitted to hidden boards. A refresh shows removed-board drafts as unavailable in Review queued. Submitting still processes other authorized boards; a refused board gets a clear visibility error and retains its drafts. The page never reports full success or closes while any submitted board is refused; Aaron can explicitly clear its actions or restore visibility.

The PWA uses its device token only in an authorization header and navigates in its current browsing context to a same-origin relative path plus `#t=<ticket>`. The page strips the fragment before any request, exchanges it once, and keeps the session only in sessionStorage. It never reads the PWA token. TUI `o` retains the same transport; both sessions and tickets die on daemon restart. A role/focus URL parameter never grants access.

### Page layout and navigation

Use a desktop two-pane layout: coordinator rail on the left, selected board in the main pane. The rail lists exactly the visible coordinator projection of the Hera rail in its existing order, with canonical parent nesting for sub-coordinators only. Plain workers never appear. Use the same ordering policy (including pinned/kanban order); omit section headings and fold controls as the named presentation simplification in Non-goals. This is one view, with no entry-mode switch, coordinator-directory screen or all-board card stream.

Each nav row shows small open Decisions/FYIs counts such as `2 · 1`, labeled accessibly by kind. Zero-item rows show no counts and are greyed, but remain fully selectable. The selected row has a distinct marker even when greyed. Empty boards show `Nothing open for <name>` in either tab, with both zero-count tabs still available. Nav membership/order does not depend on board counts; rows do not vanish after dismissal.

TUI `o` preselects its current coordinator, including an empty board. The phone selects the first coordinator with open items in rail traversal order, or the first row if all are empty. No coordinators yields a whole-page empty state. Main-pane tabs show counts only for the selected board. On initial selection choose Decisions unless it has no decisions and has FYIs; a valid focus selects the item’s kind and scrolls/highlights it. User-chosen tab is retained when moving to another coordinator, even if that tab is empty.

Keep decision option cards, required plain/other notes, recommendation shortcuts and tally within Decisions. FYIs show subject/body using the same safe renderer, comment box and exclusive pending Comment, Dismiss or Comment and dismiss action. Typing alone does not queue an action. Comment actions require non-whitespace notes; dismiss requires none. Switching boards/tabs never discards drafts or changes the scope of the queued submission. One persistent Review queued control and one Submit actions control cover queued actions on every board and tab, including invalid notes that must be fixed explicitly; unavailable boards receive explicit refusal results without blocking other authorized board batches.

### Coordinator decisions: keyboard and phone behavior

- **Every row is navigable:** Cmd+Up/Down traverses the depth-first rail order including greyed coordinators/sub-coordinators; it clamps at the first/last row. This matches rail movement and avoids jumps when a board empties.
- **Board tabs:** Cmd+Left selects Decisions and Cmd+Right selects FYIs. Ctrl+Up/Down/Left/Right are desktop aliases for cross-platform use. Ignore events with additional Alt/Shift modifiers or active composition.
- **Text editing wins:** Never intercept any of these shortcuts in input, textarea, select, contenteditable or textbox-role controls (including nested descendants), or during IME composition. Preserve default browser behavior for unhandled events. Handled events prevent native scroll/history actions; navigation leaves focus on the navigation context rather than moving it into a note.
- **Phone tree:** At narrow width replace the left rail with a top coordinator button opening a drawer containing the identical tree, counts and greyed selectable rows. Choosing a row closes the drawer; Close/Escape dismisses it, with focus restored to the trigger, and the modal traps focus while open. No phone keyboard shortcut requirement.
- **Phone tabs:** The visible tab bar always works. Horizontal swipes on non-editable, non-interactive main-pane background may also select the adjacent tab; vertical scrolling, text selection and note/option interaction remain native. Keyboard/swipe/tab changes preserve all queued responses.
- **Submission:** Keep all queued answers until explicit submit, regardless of current coordinator. Review queued names the owning coordinator for every action; one successful submission covers all queued boards.

### FYI persistence, notice and retries

Use a new per-role `POST .../board/fyi-actions` batch, with `action_id`, `item_id`, `expected_updated_at`, `action` and `note`. Store successful action receipts durably, scoped by role and action ID, with a normalized payload fingerprint, outcome and full comment. A duplicate identical action returns its stored result with no write or second notice; reuse with a different payload is invalid. Retain receipts with the board history rather than expiring them while retries might still occur. Read-only receipt replay precedes item staleness/closed checks. Decision answers retain their existing endpoint and resolved-item retry behavior.

Comment-only leaves the item's resolved/read state and `updated_at` unchanged: it is human feedback, not a material edit to the coordinator's text. Its durable receipt stores the full comment for restart/recycle and truncation recovery. Dismiss sets resolved by human, clears resolution choice, bumps `updated_at`, and emits no coordinator message. Comment-and-dismiss does both atomically, recording the note as the resolution note as well as on the receipt. A transaction commits successful receipts, dismissals and at most one FYI summary per role batch to the existing board-notice outbox; rollback commits none. Ordinary item errors do not block other valid actions. Never also invoke the decision dropped-notice path.

Summaries explicitly distinguish still-open comments from already-dismissed FYIs, include full-note receipt references when shortened, and list that coordinator's remaining open IDs of both kinds. Separate answer/FYI batches may produce separate summaries; never combine recipients across coordinators. Outbox delivery retains its existing exactly-once semantics, `Aaron (board)` attribution and deferred delivery without a binding. A silent dismissal's receipt must not itself enqueue a notice.

### Multi-request completion and recovery

Snapshot the full set of queued actions when submitting, disable duplicate concurrent submits and partition by role/kind in batches of at most 16. Give every FYI action a stable generated ID before its first send and persist that ID with its draft until acknowledged. A network failure keeps the same ID and immutable payload for retry, including across reload; editing its payload requires first reconciling that uncertain result. A revised action after a definitive conflict or invalid result gets a new action ID. Confirm a matching response for every submitted action: decision success is resolved/already_resolved; comment success is commented; dismissal success is resolved/already_resolved. Comment on an already-closed item is not successful because no feedback was delivered. An accepted comment remains visibly open if another submitted action fails, but its pending action/draft is cleared to prevent resubmission.

Close only after every submitted action across every batch succeeds, regardless of untouched open items. A failure in a later coordinator cannot undo earlier commits; display their confirmed results and retry only unacknowledged/failed work. A transport failure stays unresolved until an idempotent retry confirms it. On browser/PWA close refusal show `Actions sent — you can close this tab`, counts of accepted answers, comments and dismissals, and a same-origin Return to Argus link. Stop polling and fence late results after completion.

Poll navigation metadata and mixed-item reads every 20 seconds while visible, with the existing refresh banner rather than replacing edits. Visibility revocation is enforced immediately by the server even before accepted refresh. Persist drafts by role/item/revision, including FYI action and ID; keep the existing conflict behavior of retaining note text but clearing selection/action. The Copy answers control becomes Copy responses and includes decisions and FYIs with coordinator labels. An expired phone session offers Return to Argus to relaunch; a TUI session retains the press-`o` instruction. Neither page reads or copies the PWA's device credential. New session context must supersede any old stored session, and credentials remain sessionStorage-only.

### PWA entry, parity and cache

Add a global `Decide` entry alongside Tasks, Projects, + New and Settings; verify the narrow phone navigation can accommodate it without overflow. Its badge is the sum of open decisions and FYIs across eligible coordinators (not unread counts). `GET /api/hera/board/decision-counts` is master/device-only and returns totals plus per-role counts. Refresh on app entry/foreground and the existing visible polling cadence; no new push dependency. Keep entry available at zero so Aaron can see an empty page. A failed badge fetch shows unavailable rather than a misleading zero; a failed launch stays in the PWA with retry feedback.

Frontend evaluation: TUI launches the common page with its coordinator preselected, including empty/FYI-only boards; web receives the same rail/drawer page and mixed-action UI; macOS is explicitly excluded at Aaron's direction, superseding a follow-up for this work. Update the README Reference endpoints and launch description. No key binding is added, removed or rebound. Preserve the service worker's network-only `/decide` behavior and exclusion from shell assets; API responses containing data or credentials are not cached. Bump `SW_VERSION` when changing the PWA/decide shell assets in the implementation PR, using the then-current version rather than pinning today's value.

## Risks / trade-offs

- Both launches can affect all currently rail-visible boards → explicit rail-board auth class, shared canonical visibility checks on every request, eight-hour cap and strict board-only endpoint allowlist. This intentionally replaces the old one-board TUI authorization scope.
- Visibility can change while answers are queued → accepted refresh reconciles nav without overwriting notes; inaccessible drafts stay marked unavailable; their writes are refused clearly while other board batches proceed, and overall success stays blocked until those submitted failures are handled.
- Several role batches can partly commit → preserve per-action results and retry IDs; never report full success or close on a missing result.
- Durable comment receipts add storage → comments are bounded to 4 KiB, receipts share board-history retention and provide retry/truncation recovery.
- PWA navigation can be refused offline or closing can fail → explicit launch errors and sent confirmation/return link; do not cache board data as an offline substitute.

## Migration plan

After Aaron approves, implement with failing tests first. Add receipt persistence and API/session contracts before page/PWA wiring. Existing role launch and answers payloads keep working, with role launch now selecting rather than restricting a board; no legacy data migration or compatibility layer is needed. Daemon restart clears tickets/sessions as today, but receipts and comments survive. Roll back UI/API together if needed; preserved receipts are durable data. Archive this change with the implementation in the same PR before merge, not in this spec-only delivery.

## Approval status and remaining question

The layout question is settled: Aaron selected the shared coordinator rail with a responsive phone drawer. The earlier grouped-stream and two-entry-mode proposals are superseded.

- **Session scope decision (coordinator-approved):** Both launches use the explicit rail-board session class, replacing the old one-coordinator snapshot. TUI role is only the starting selection. Every read/write rechecks current visibility; hidden/archived-board writes fail clearly while other authorized board submissions continue. No broader API authorization is granted.
- **Web entry recommendation:** Keep the global Decide entry and total open-count badge so the phone can reach the queue without first choosing a project.
- **Presentation simplification:** Reuse canonical rail ordering and nesting without reproducing kanban group headings/fold controls; this is a named Non-goal, not a difference in coordinator membership or traversal order.
- **Implementation approval:** The layout decision authorizes these spec and mockup revisions; implementation still awaits Aaron’s approval.
