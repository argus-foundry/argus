## MODIFIED Requirements

### Requirement: Scoped launch and session

The page SHALL read a launch ticket from the URL fragment, remove it from address bar/history with history.replaceState before any network request, exchange it once for a decision session and store the session only in sessionStorage. A new ticket SHALL supersede old stored context. TUI and phone SHALL receive the same rail-board session class: eight-hour absolute lifetime and access limited to board navigation and permitted board read/answer/FYI action endpoints for currently rail-visible coordinators. A TUI role URL parameter SHALL choose initial selection, never restrict or grant authorization. The server SHALL recheck current rail visibility on every request; hidden, archived, nuked and non-coordinator targets SHALL be denied. The page SHALL NOT read the PWA device credential. On expired/missing/rejected sessions it SHALL preserve drafts and Copy responses, offering Return to Argus for phone launches or press-o relaunch guidance for TUI launches. Neither ticket nor session SHALL appear in query strings, localStorage or logs.

#### Scenario: Fragment removed

- **WHEN** the page opens with a launch fragment
- **THEN** it removes the fragment before exchange and neither address bar nor history entry retains it

#### Scenario: TUI preselection is not a permission boundary

- **WHEN** o opens coordinator 12 and the user selects another visible coordinator
- **THEN** the same session may read and answer that visible board but no general API routes

#### Scenario: Visibility revoked

- **WHEN** a selected coordinator becomes hidden or archived after page load
- **THEN** further board requests are denied, choice-only drafts are dropped and written notes/annotations remain recoverable in a collapsed disclosure

#### Scenario: Expired session and reload

- **WHEN** the page reloads within its session lifetime
- **THEN** it reuses the stored session and drafts; if rejected, drafts and Copy responses remain with relaunch guidance

### Requirement: Round contents and layout

The page SHALL be one shared two-pane view: a desktop left coordinator nav and one selected coordinator’s board in the main pane. The nav SHALL mirror the Hera rail’s visible coordinator membership, canonical parent nesting and traversal order, excluding archived, hidden or nuked coordinators/orchestrators and all plain workers. Only sub-coordinators SHALL appear as children. Rail-induced pinned/kanban ordering SHALL be preserved; kanban section headings/fold controls are an intentional presentation non-goal. Every visible coordinator SHALL remain listed when empty, shown greyed out with no count labels and still selectable. Non-empty rows SHALL show small open decision and FYI counts, with accessible kind labels. The selection indicator SHALL remain distinct on greyed rows. Item counts SHALL NOT reorder or remove coordinators.

The main pane SHALL expose Decisions and FYIs tabs showing the selected board’s open counts, never aggregate tab counts. Open cards SHALL be ordered by item ID and resolved items omitted on initial load. An empty selected coordinator SHALL show `Nothing open for <name>` in both tabs except for the transient confirmation after Submit. Both tabs SHALL stay available with zero counts. TUI launch SHALL select the requested visible coordinator even when empty; phone launch SHALL select the first non-empty coordinator in nav order, or the first row if all are empty. With no visible coordinators the page SHALL show a whole-page empty state. Initial tab SHALL be Decisions unless only FYIs are open; a valid focus SHALL select the item’s tab and scroll/highlight it. Subsequent coordinator changes SHALL preserve the user-selected tab and all drafts/queued actions.

A structured decision SHALL render its subject as question, body as situation, selectable option cards with label, summary, Upside and Downside rows, Recommended pill/reason, dashed Something else card and optional note. A plain decision SHALL show subject, body and required note. The Decisions tab SHALL retain the sticky `N of M decided` tally and jump segments, and a take-recommendations button selecting only recommended undecided decisions on the selected board. FYIs SHALL render subject/body with the same safe text/link renderer, comment field and exclusive Comment, Dismiss, or Comment and dismiss pending action. Typing alone SHALL NOT queue an FYI action. Comment actions SHALL require non-whitespace notes. One persistent Review queued control and two submission controls, Submit and Submit & close, SHALL cover all queued actions across boards and tabs. Submission SHALL require at least one queued action and valid required notes for every queued card, including hidden tabs and unselected boards.

Review queued SHALL be explicit, contain only actually queued responses grouped by coordinator, and use left-aligned content, one status line per item and small left-aligned action buttons. The footer SHALL omit cumulative confirmed counts, use correct singular board/coordinator labels, and disable submission when nothing is queued.

Saved annotations SHALL additionally queue feedback and satisfy comment content as defined by Queued annotation feedback; the original typing-only and answer-note rules remain for unsaved text and answers. FYI Dismiss with saved annotations SHALL be visibly Comment and dismiss. Annotation-only decision comments SHALL use the same global review/submit controls.

#### Scenario: Membership mirrors the rail

- **WHEN** Hera shows visible coordinators, a hidden coordinator, an archived coordinator and plain workers
- **THEN** the page lists exactly the visible coordinators in the rail’s order and no hidden/archived coordinator or worker

#### Scenario: Nested sub-coordinator

- **WHEN** a visible coordinator owns a sub-coordinator plus plain workers
- **THEN** the sub-coordinator is indented beneath its canonical parent and workers are absent

#### Scenario: Greyed empty row

- **WHEN** a coordinator has no open decisions or FYIs, including after its last item is dismissed
- **THEN** its nav row stays in place, greyed with no counts and still selectable, and either tab shows `Nothing open for <name>` outside the transient submit confirmation

#### Scenario: Entry selection

- **WHEN** TUI o targets an empty visible coordinator while another coordinator has open items
- **THEN** that empty coordinator is selected; a phone launch instead selects the first non-empty coordinator in rail order

#### Scenario: Selected-board counts

- **WHEN** the selected board has two decisions and one FYI, and another has three FYIs
- **THEN** tabs show Decisions (2) and FYIs (1), while each nav row shows only its own kind counts

#### Scenario: Something else needs a note

- **WHEN** Something else is selected with an empty note and the user changes coordinator
- **THEN** the draft is kept and global submission stays disabled until the note is supplied or the action is cleared

#### Scenario: Focused FYI

- **WHEN** TUI launch focuses an open FYI
- **THEN** its coordinator and FYIs tab are selected and the card is highlighted

#### Scenario: Empty roster

- **WHEN** no coordinator is visible under rail rules
- **THEN** the page shows a no-coordinators empty state without inventing a selectable board

### Requirement: Partial submit and per-item results

Submit SHALL snapshot only explicitly queued answers and FYI actions across all tabs/coordinators, including each item's loaded `expected_updated_at`, and SHALL leave all unqueued items untouched. FYI actions SHALL carry stable action IDs and immutable payloads retained on uncertain outcomes and reload; the page SHALL reconcile an uncertain result before allowing payload edits and SHALL generate a new ID for a revised action after a definitive conflict/invalid result. The page SHALL partition requests by owning role and endpoint in batches of at most 16, prevent concurrent duplicate submits and match every submitted action to a result. When every submitted action succeeds, Submit SHALL keep the page open and refresh boards/counts, while Submit & close SHALL finish even if untouched items remain open. Both controls SHALL use the same immutable payloads, action IDs and uncertain-result recovery, and be disabled under the same conditions. Confirmed queued drafts SHALL clear. Submit SHALL remain on the selected coordinator and tab, refresh rail/tab counts and show a calm Answers sent to Argus confirmation, compact above remaining cards or alone in an empty tab. The note SHALL disappear on navigation or the next edit. A partial result SHALL show one short sent/needs-attention count; failures SHALL appear only on their own board cards, with rail and tab attention markers for failures elsewhere. No submit SHALL automatically open Review queued or advance coordinator. Otherwise it SHALL stay open and apply results per card: resolved decisions and dismissed FYIs disappear from their open tabs; accepted comments remain open but their queued action/draft is cleared and marked sent; conflicts replace current content, preserve note text, clear the pending choice/action and flag the edit; already_resolved/not_found cards are marked closed; invalid cards show an error. A comment-only action or undelivered written note against an already-resolved item SHALL need attention and remain recoverable rather than count as delivered. Confirmed actions SHALL NOT be resubmitted during retry of failed or unconfirmed work. The page SHALL report accepted action counts and remaining failures, never overall success on a partial failure.

Snapshots and immutable retries SHALL include annotations and required decision response action IDs. Decision comment outcomes SHALL accept commented, clear confirmed feedback and leave the item open. Fresh feedback rejected on closed items SHALL retain drafts for attention; already_resolved SHALL NOT confirm undelivered annotations. Conflicts SHALL keep all original types, ranges, quotes/comments in recoverable stale drafts and require reanchoring or removal before requeue.

#### Scenario: Partial answer round

- **WHEN** three of five decisions are queued and all three resolve
- **THEN** those three resolve and two remain open; Submit refreshes the page in place, while Submit & close finishes

#### Scenario: Coordinator edited a card mid-round

- **WHEN** a decision or FYI was revised after load
- **THEN** its action conflicts without mutation, its note is preserved with selection cleared, and unrelated valid actions succeed

#### Scenario: Item answered elsewhere

- **WHEN** a decision answer or FYI dismissal finds its item already resolved
- **THEN** already_resolved counts as accepted without creating another notice when the draft has no undelivered written feedback; written notes/annotations remain recoverable instead

#### Scenario: Comment remains open during partial failure

- **WHEN** a comment succeeds for one coordinator and another coordinator's answer fails
- **THEN** the page stays open, shows the comment as sent on the still-open FYI and retries only failed or unconfirmed work

#### Scenario: One queued board becomes hidden or archived

- **WHEN** one coordinator becomes hidden or archived while its answers and another coordinator’s answers are queued
- **THEN** writes to the unavailable board are refused with a clear message and written drafts are kept recoverably, the other coordinator’s valid actions still submit, and the page stays open showing partial results

#### Scenario: Comment response lost

- **WHEN** a comment commits but its response is lost and the page reloads
- **THEN** retry uses the original action ID and confirms the stored outcome without sending another coordinator notice

### Requirement: Live change awareness and drafts

While visible and unfinished the page SHALL poll scoped navigation metadata and mixed-item endpoints every 20 seconds. Changes SHALL produce a Refresh banner without replacing editing cards or drafts. Accepted refresh SHALL reconcile the visible rail tree using the canonical visibility/order policy; server-side visibility revocation SHALL take effect even before refresh. Drafts for closed or unavailable items SHALL never block submission: choice-only drafts SHALL be dropped; notes/annotations SHALL remain in one collapsed recovery line per item with Copy and Discard. Frozen uncertain payloads SHALL remain immutable until reconciled. A submit SHALL refuse writes to those boards with a clear hidden/archived/unavailable message while still submitting queued actions for other authorized coordinators. Refused batches SHALL prevent overall success/close for that submit and retain written feedback recoverably. Selection SHALL stay on the same role if still visible, otherwise move to the first non-empty remaining row (or first row when all empty). Drafts SHALL persist in `localStorage` keyed by role, item ID and item revision, containing decision choice/note or FYI pending action/note/action ID; no credentials SHALL be stored there. Explicit refresh after a coordinator revision SHALL clear the queued choice/action while preserving written notes; conflict results SHALL retain note text as described in partial results. Unrevised drafts SHALL survive reload, tab changes and session expiry. While unfinished, Copy responses SHALL copy or expose selectable plain text for all queued decisions/FYIs across tabs, labeled by coordinator and action.

For annotation-bearing drafts, an accepted refresh after revision SHALL retain the prior note/annotations as a recoverable stale draft instead of discarding them. It SHALL clear the queued choice/action and require explicit reanchoring or removal before submit. Frozen uncertain payloads SHALL never be discarded or changed by refresh.

#### Scenario: New item arrives

- **WHEN** an existing scoped coordinator adds a decision or FYI
- **THEN** polling offers Refresh without changing edits; a newly visible coordinator appears in canonical rail order only after accepted refresh

#### Scenario: Revised item discards its draft

- **WHEN** the coordinator revises an item and the user accepts Refresh
- **THEN** that card clears its queued choice/action and keeps written notes/annotations recoverable while unrevised cards keep their drafts

#### Scenario: Copy fallback

- **WHEN** Copy responses is used on an unfinished mixed round
- **THEN** decisions and FYI actions/notes across tabs are copied or selected for manual copying with coordinator labels

### Requirement: Receipt-safe completion through the shared queue

Done/Dropped actions SHALL use the existing global response queue, review/copy summary, guarded draft persistence, per-item revision checks, role-batched submissions and durable receipts. Failed or uncertain submissions SHALL retain drafts and retry the identical action ID and frozen payload. Accepted completions SHALL clear only confirmed drafts and count toward Submit & close completion. A stale revision SHALL preserve notes while requiring explicit reconfirmation. Existing decision answers, FYI comments/dismissal and annotation behavior SHALL remain unchanged; annotation creation for the new kinds is outside this change.

#### Scenario: Mixed round

- **WHEN** Aaron queues a decision answer, FYI comment, action Done and board todo Dropped
- **THEN** both submit controls process every queued operation with correct summaries and only confirmed results clear drafts

#### Scenario: Uncertain completion retry

- **WHEN** the daemon commits Done but the response is lost before reload
- **THEN** the saved action ID/payload replay the stored receipt without another completion or coordinator notice

#### Scenario: Stale action

- **WHEN** the coordinator changes an action's step after Aaron queues Done
- **THEN** submission reports conflict, preserves the optional note and requires confirmation of the new revision

### Requirement: Accurate partial response feedback

Decide SHALL explain a result containing an already-resolved item as closed feedback that was not saved rather than bare invalid. It SHALL display a missing/oversized note warning only when the note is actually required and empty or exceeds its byte limit. A conflict that clears the pending response SHALL preserve the note and show the item-changed state without falsely claiming the note is missing. Unchanged items SHALL clear on confirmed success despite sibling conflicts, and re-answering conflicted items SHALL submit the new revision with a new action ID.

#### Scenario: Two coordinators with mixed revisions

- **WHEN** four responses are submitted across two coordinators and two items changed during review
- **THEN** unchanged responses are accepted, only the changed items show the stale state, and re-answering those two items succeeds without resending the accepted responses

#### Scenario: Closed FYI comment

- **WHEN** an FYI comment result identifies its item as resolved
- **THEN** the page explains that the item is already resolved and the comment was not saved, retaining the note for recovery


#### Scenario: Board landing and remaining cards

- **WHEN** Submit accepts every queued response with unanswered items in the same or other tabs
- **THEN** the selected board/tab stays selected, accepted cards disappear, counts refresh, and Answers sent to Argus appears alone or above remaining cards; other tabs only show their counts

#### Scenario: Local and remote failures

- **WHEN** a mixed submit fails on the current tab or another coordinator/tab
- **THEN** the current board stays visible with a short sent/needs-attention note; each failed card keeps its own explanation and draft, and its tab/rail shows an attention marker

#### Scenario: Closed feedback recovery

- **WHEN** a queued item closes or its coordinator becomes unavailable
- **THEN** an empty choice/Done/dismiss draft is dropped, written notes and annotations survive in one collapsed line per item with Copy and Discard, and neither blocks further submissions

#### Scenario: Explicit queue review

- **WHEN** Aaron activates Review queued
- **THEN** only actually queued responses appear, grouped by coordinator, left aligned with one status line and small left-aligned actions per item

#### Scenario: Footer and close behavior

- **WHEN** there are zero or one queued responses or a submission finishes
- **THEN** the footer has no cumulative Confirmed line, uses singular board/coordinator where appropriate, disables Submit when empty, and Submit & close closes only on full success

#### Scenario: Harmless work item touches

- **WHEN** an action/todo is marked read, reordered, or only trigger_pending changes after Done is queued
- **THEN** its authored revision stays stable and Done is accepted with unchanged receipt fingerprint/replay semantics; an authored step edit still conflicts

### Requirement: Finish successful submissions

Submit SHALL never attempt to close the page. It SHALL refresh boards/counts after confirmed actions, remove resolved items, clear confirmed queued drafts, and show a transient confirmation on the selected coordinator/tab with partial failures on their affected cards so further work can continue. Submit & close SHALL attempt `window.close` only after every submitted action across all role batches has a matching accepted result. Accepted decision outcomes SHALL be resolved/already_resolved; accepted FYI comment outcomes SHALL be commented; accepted FYI dismissal/comment-and-dismiss outcomes SHALL be resolved/already_resolved. Replay of a stored successful result SHALL count as accepted. Untouched decisions/FYIs and successfully commented FYIs remaining open SHALL NOT block completion. If closing throws or leaves the page open, the page SHALL replace its action controls with `Actions sent — you can close this tab`, accepted counts for answers/comments/dismissals and a same-origin Return to Argus link. Any conflict, invalid, not_found, missing/mismatched result, unaccepted outcome or failed request SHALL keep the page open with confirmed results and drafts. Completion SHALL stop polling and fence late responses from removed controls.

Accepted decision comment outcomes SHALL additionally be commented. Annotation-bearing actions SHALL require an explicit receipt-confirmed success; already_resolved without delivery confirmation SHALL NOT count as accepted feedback.

#### Scenario: Mixed successful submit

- **WHEN** Submit & close confirms all submitted answers, comments and dismissals while untouched items and comment-only FYIs remain open
- **THEN** the page attempts to close

#### Scenario: Browser refuses closing

- **WHEN** Submit & close confirms every submitted action but closing is refused or throws
- **THEN** the sent confirmation, accepted counts and Return to Argus link replace the action controls

#### Scenario: Submission needs attention

- **WHEN** any role batch fails or any submitted action has an error or missing result
- **THEN** no close attempt or overall sent confirmation occurs and confirmed results and remaining drafts stay available

#### Scenario: Continue after submit

- **WHEN** Submit confirms queued answers and annotation-only comments
- **THEN** resolved items disappear, confirmed drafts clear, a transient board confirmation appears, boards/counts refresh and the page stays open for further work

#### Scenario: Shared submit recovery

- **WHEN** either submission control receives a partial failure or an uncertain result
- **THEN** the page stays open and retains the identical action ID and frozen payload until reconciled, using the same disabled conditions and retry path


### Requirement: Annotation draft recovery and limits

Annotations SHALL persist in the same guarded localStorage response records as note drafts, bound to role, item and updated_at. Each COMMENT SHALL require non-whitespace text at most 4096 UTF-8 bytes; DELETION SHALL allow an empty comment (any supplied comment SHALL be at most 4096 UTF-8 bytes); at most 20 annotations of all types combined SHALL be saved per item. The UI SHALL retain editor text on validation/storage failure and expose the limit/error. Drafts SHALL survive reload, tab/board changes and expiry. Frozen uncertain payloads SHALL include annotations and action IDs, remain immutable until reconciled, and retry identically. A conflict or accepted refresh changing the item revision SHALL retain notes, old anchors and quotes in a recoverable stale draft, clear the queued choice/action and require explicit reanchoring or removal before resubmission. No stale annotation SHALL be silently discarded or automatically rebound. Accepted actions SHALL clear only their confirmed drafts; failures and visibility refusals SHALL prevent overall finish for that round and retain written feedback in collapsed recovery when closed/unavailable. All-confirmed annotation-only comments SHALL keep the decision open. Submit SHALL keep the page open; Submit & close SHALL finish even though items remain open.

#### Scenario: Byte and count limits

- **WHEN** Aaron exceeds 4096 UTF-8 bytes or attempts a twenty-first saved annotation
- **THEN** Save explains the applicable limit and preserves typed text; exactly 20 within-cap comments are permitted

#### Scenario: Reload and expiry

- **WHEN** Aaron reloads, navigates boards/tabs or loses the session
- **THEN** saved annotations and quotes remain recoverable without credentials in localStorage

#### Scenario: Uncertain retry

- **WHEN** the server commits annotations but the response is lost before reload
- **THEN** the original ID and immutable payload replay; edits/removal wait and no second feedback is sent

#### Scenario: Changed item

- **WHEN** a conflict result or accepted refresh supplies a new updated_at
- **THEN** old note and annotations remain recoverable, choice/action clears and Aaron must reanchor or remove stale annotations explicitly

#### Scenario: Partial acceptance

- **WHEN** one annotated item succeeds while another conflicts or loses visibility
- **THEN** only confirmed drafts clear and the page stays open with recoverable failed feedback

#### Scenario: Comment completion

- **WHEN** every queued annotation-only decision comment is confirmed
- **THEN** the existing completion flow runs and those decisions remain open
