## MODIFIED Requirements

### Requirement: Scoped launch and session

The page SHALL read a launch ticket from the URL fragment, remove the fragment from the address bar and history entry with `history.replaceState` before any network request, exchange the ticket once for a decision session, and keep the session in `sessionStorage`. A new ticket SHALL supersede any previously stored session context. Sessions SHALL contain one role ID for TUI launches or an immutable explicit set of coordinator role IDs for PWA launches. The page SHALL send the session only to permitted decision/answer, mixed-item read and FYI action endpoints for those roles. It SHALL NOT read the PWA device credential. When the session is missing, expired or rejected it SHALL keep drafts, offer Copy responses and show relaunch guidance: Return to Argus for a PWA launch and press the board's decide key again for a TUI launch. Neither ticket nor session SHALL appear in any URL query string, `localStorage`, daemon log or `uxlog` line.

#### Scenario: Fragment removed

- **WHEN** the page loads with `#t=<ticket>`
- **THEN** the fragment is removed before exchange and neither the address bar nor the history entry holds the ticket

#### Scenario: Expired phone session

- **WHEN** a PWA-launched session is rejected with 401
- **THEN** drafts and Copy responses remain available and Return to Argus lets the user relaunch without a TUI keypress

#### Scenario: Reload

- **WHEN** the user reloads within the session lifetime without a new ticket
- **THEN** the page reuses that session and its drafts without widening its role set

### Requirement: Round contents and layout

The page SHALL expose Decisions and FYIs tabs, each labeled with its open count, grouping cards by coordinator within the selected tab. A TUI session SHALL show one coordinator; a phone session SHALL show every eligible coordinator in its session scope. Groups SHALL be identified by orchestrator, coordinator name and role ID, ordered by role ID, and show both kind counts; cards SHALL be ordered by item ID. Open counts SHALL count open items, not unread items. Resolved items SHALL NOT appear on initial load. Both tabs SHALL remain visible at zero. Decisions SHALL be selected initially unless only FYIs are open. A valid `focus` item ID SHALL select its kind's tab, scroll to and highlight the card without hiding other cards.

A structured decision SHALL render its subject as question, body as situation, selectable option cards with label, summary, Upside and Downside rows, Recommended pill/reason, dashed Something else card and optional note. A plain decision SHALL show subject, body and required note. The Decisions tab SHALL retain the sticky `N of M decided` tally and one jump segment per decision, and the take-recommendations button when undecided cards have recommendations; that button SHALL only select recommended undecided decisions. FYIs SHALL render subject/body with the same safe text/link renderer, comment field and exclusive Comment, Dismiss, or Comment and dismiss pending action. Merely typing a comment SHALL NOT queue it. Comment actions SHALL require non-whitespace notes. Tab switching SHALL retain drafts. Submit actions SHALL require at least one queued action and valid required notes on all queued cards, including hidden tabs.

#### Scenario: Mixed round

- **WHEN** two coordinators have three open decisions and two open FYIs in total
- **THEN** the tabs show Decisions (3) and FYIs (2), with each coordinator's cards grouped and kind counts shown

#### Scenario: Something else needs a note

- **WHEN** Something else is selected with an empty note
- **THEN** its card shows a required-note hint and submission is disabled even if the user switches tabs

#### Scenario: Take the recommendation

- **WHEN** three undecided decisions include two recommendations
- **THEN** the shortcut selects only those two decisions and does not queue any FYI action

#### Scenario: Focused FYI

- **WHEN** a TUI launch focuses an open FYI
- **THEN** FYIs is selected and that card is highlighted without hiding other open items

#### Scenario: Empty and FYI-only boards

- **WHEN** no decisions and two FYIs are open
- **THEN** FYIs is initially selected, Decisions (0) stays accessible, and an entirely empty session shows both zero-count tabs and an empty state

### Requirement: Partial submit and per-item results

Submit SHALL snapshot only explicitly queued answers and FYI actions across all tabs/coordinators, including each item's loaded `expected_updated_at`, and SHALL leave all unqueued items untouched. FYI actions SHALL carry stable action IDs and immutable payloads retained on uncertain outcomes and reload; the page SHALL reconcile an uncertain result before allowing payload edits and SHALL generate a new ID for a revised action after a definitive conflict/invalid result. The page SHALL partition requests by owning role and endpoint in batches of at most 16, prevent concurrent duplicate submits and match every submitted action to a result. When every submitted action succeeds it SHALL finish even if untouched items remain open. Otherwise it SHALL stay open and apply results per card: resolved decisions become read-only Answered; dismissed FYIs become read-only Dismissed; accepted comments remain open but their queued action/draft is cleared and marked sent; conflicts replace current content, preserve note text, clear the pending choice/action and flag the edit; already_resolved/not_found cards are marked closed; invalid cards show an error. A comment-only action against an already-resolved item SHALL need attention rather than count as delivered. Confirmed actions SHALL NOT be resubmitted during retry of failed or unconfirmed work. The page SHALL report accepted action counts and remaining failures, never overall success on a partial failure.

#### Scenario: Partial answer round

- **WHEN** three of five decisions are queued and all three resolve
- **THEN** those three resolve, two remain open and the page finishes

#### Scenario: Coordinator edited a card mid-round

- **WHEN** a decision or FYI was revised after load
- **THEN** its action conflicts without mutation, its note is preserved with selection cleared, and unrelated valid actions succeed

#### Scenario: Item answered elsewhere

- **WHEN** a decision answer or FYI dismissal finds its item already resolved
- **THEN** already_resolved counts as accepted without creating another notice

#### Scenario: Comment remains open during partial failure

- **WHEN** a comment succeeds for one coordinator and another coordinator's answer fails
- **THEN** the page stays open, shows the comment as sent on the still-open FYI and retries only failed or unconfirmed work

#### Scenario: Comment response lost

- **WHEN** a comment commits but its response is lost and the page reloads
- **THEN** retry uses the original action ID and confirms the stored outcome without sending another coordinator notice

### Requirement: One summary message to the coordinator

When an answer submission resolves at least one decision, the system SHALL write exactly one self-addressed coordinator message atomically with those resolutions, delivered through the board-notice outbox and shown as `Aaron (board)`. It SHALL list resolved decisions in ID order with ID, subject, chosen option label (marking recommendation or Something else), note or none, and that coordinator's remaining open IDs of both kinds. It SHALL state each answer is already resolved. An answer submission resolving nothing SHALL write no message and SHALL NOT also write the human-dismissal dropped notice. Full answer notes up to 4 KiB SHALL remain on each item; summaries SHALL fit 64 KiB by evenly budgeting notes and ending shortened notes with `… [truncated — full note on board item #N; hera_board_list include_resolved=true]`. Notes within budget SHALL remain untouched. FYI batches SHALL follow the separate durable FYI comment contract, producing at most one FYI summary per owning coordinator and no message for dismiss-only batches. No summary SHALL include another coordinator's board. All notices SHALL remain pending durably when no binding exists and deliver once a binding exists.

#### Scenario: One message for an answer round

- **WHEN** one coordinator's submission resolves four decisions
- **THEN** exactly one answer summary lists those answers and its remaining open decision and FYI IDs

#### Scenario: Atomic with resolution

- **WHEN** the process exits after commit but before delivery
- **THEN** resolutions and pending notice both exist and the notify tick delivers later

#### Scenario: No message when nothing resolved

- **WHEN** every decision answer conflicts
- **THEN** no answer summary is written

#### Scenario: Maximum-size notes

- **WHEN** sixteen answers each carry a 4 KiB note
- **THEN** full notes persist and the summary fits 64 KiB with full-note pointers on shortened notes

#### Scenario: Coordinator unbound

- **WHEN** an answer or comment commits without a live coordinator binding
- **THEN** the corresponding summary remains durable for later delivery

### Requirement: Live change awareness and drafts

While visible and unfinished the page SHALL poll mixed-item endpoints for its scoped coordinators every 20 seconds. Changes SHALL produce a Refresh banner without replacing or reordering editing cards. Polling SHALL NOT widen session scope. Drafts SHALL persist in `localStorage` keyed by role, item ID and item revision, containing decision choice/note or FYI pending action/note/action ID; no credentials SHALL be stored there. Explicit refresh after a coordinator revision SHALL discard stale drafts; conflict results SHALL retain note text as described in partial results. Unrevised drafts SHALL survive reload, tab changes and session expiry. While unfinished, Copy responses SHALL copy or expose selectable plain text for all queued decisions/FYIs across tabs, labeled by coordinator and action.

#### Scenario: New item arrives

- **WHEN** an existing scoped coordinator adds a decision or FYI
- **THEN** polling offers Refresh without changing edits or adding new role scope

#### Scenario: Revised item discards its draft

- **WHEN** the coordinator revises an item and the user accepts Refresh
- **THEN** that card loses its stale draft while unrevised cards keep theirs

#### Scenario: Copy fallback

- **WHEN** Copy responses is used on an unfinished mixed round
- **THEN** decisions and FYI actions/notes across tabs are copied or selected for manual copying with coordinator labels

### Requirement: Observability

Launch, exchange, count fetch, mixed-item read, answer and FYI action paths SHALL log under `[board]` and `[decide]` with role IDs, item/action IDs, counts and outcomes, including success, failures, replay, conflicts, expired sessions and skipped work. No line SHALL contain tickets, sessions, device tokens, notes or bodies.

#### Scenario: Conflict is logged

- **WHEN** an answer or FYI action conflicts
- **THEN** its role/item and outcome are logged without note text

#### Scenario: Secrets never logged

- **WHEN** a PWA token mints a ticket and a session submits comments
- **THEN** no credential, comment or body appears in logs

### Requirement: Finish successful submissions

The page SHALL attempt `window.close` only after every submitted action across all role batches has a matching accepted result. Accepted decision outcomes SHALL be resolved/already_resolved; accepted FYI comment outcomes SHALL be commented; accepted FYI dismissal/comment-and-dismiss outcomes SHALL be resolved/already_resolved. Replay of a stored successful result SHALL count as accepted. Untouched decisions/FYIs and successfully commented FYIs remaining open SHALL NOT block completion. If closing throws or leaves the page open, the page SHALL replace its action controls with `Actions sent — you can close this tab`, accepted counts for answers/comments/dismissals and a same-origin Return to Argus link. Any conflict, invalid, not_found, missing/mismatched result, unaccepted outcome or failed request SHALL keep the page open with confirmed results and drafts. Completion SHALL stop polling and fence late responses from removed controls.

#### Scenario: Mixed successful submit

- **WHEN** all submitted answers, comments and dismissals succeed while untouched items and comment-only FYIs remain open
- **THEN** the page attempts to close

#### Scenario: Browser refuses closing

- **WHEN** every submitted action succeeds but closing is refused or throws
- **THEN** the sent confirmation, accepted counts and Return to Argus link replace the action controls

#### Scenario: Submission needs attention

- **WHEN** any role batch fails or any submitted action has an error or missing result
- **THEN** no close attempt or overall sent confirmation occurs and confirmed results and remaining drafts stay available

## ADDED Requirements

### Requirement: FYI action semantics on the page

Dismiss SHALL resolve an FYI by human without coordinator notice. Comment SHALL deliver non-empty feedback through the board-notice outbox while leaving the FYI open. Comment and dismiss SHALL atomically store feedback, resolve by human and enqueue its comment notice. Rendering FYI content SHALL preserve phone-width usability, remembered Light/Dark/System appearance, safe DOM text/HTTP(S) links, code-span exclusions and same-origin request boundaries already required for decisions.

#### Scenario: Comment without dismissal

- **WHEN** Aaron queues Comment with a note and submits successfully
- **THEN** the owning coordinator receives durable feedback and the FYI stays open

#### Scenario: Silent dismissal

- **WHEN** Aaron submits Dismiss
- **THEN** the FYI is resolved by human with no coordinator notice, matching TUI `x` on an FYI

#### Scenario: Comment and dismiss

- **WHEN** Aaron submits Comment and dismiss with a note
- **THEN** feedback and human resolution commit atomically and one FYI comment summary is queued
