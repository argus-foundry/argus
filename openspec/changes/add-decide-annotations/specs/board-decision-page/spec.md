## MODIFIED Requirements

### Requirement: Round contents and layout

The page SHALL be one shared two-pane view: a desktop left coordinator nav and one selected coordinator’s board in the main pane. The nav SHALL mirror the Hera rail’s visible coordinator membership, canonical parent nesting and traversal order, excluding archived, hidden or nuked coordinators/orchestrators and all plain workers. Only sub-coordinators SHALL appear as children. Rail-induced pinned/kanban ordering SHALL be preserved; kanban section headings/fold controls are an intentional presentation non-goal. Every visible coordinator SHALL remain listed when empty, shown greyed out with no count labels and still selectable. Non-empty rows SHALL show small open decision and FYI counts, with accessible kind labels. The selection indicator SHALL remain distinct on greyed rows. Item counts SHALL NOT reorder or remove coordinators.

The main pane SHALL expose Decisions and FYIs tabs showing the selected board’s open counts, never aggregate tab counts. Open cards SHALL be ordered by item ID and resolved items omitted on initial load. An empty selected coordinator SHALL show `Nothing open for <name>` in both tabs. Both tabs SHALL stay available with zero counts. TUI launch SHALL select the requested visible coordinator even when empty; phone launch SHALL select the first non-empty coordinator in nav order, or the first row if all are empty. With no visible coordinators the page SHALL show a whole-page empty state. Initial tab SHALL be Decisions unless only FYIs are open; a valid focus SHALL select the item’s tab and scroll/highlight it. Subsequent coordinator changes SHALL preserve the user-selected tab and all drafts/queued actions.

A structured decision SHALL render its subject as question, body as situation, selectable option cards with label, summary, Upside and Downside rows, Recommended pill/reason, dashed Something else card and optional note. A plain decision SHALL show subject, body and required note. The Decisions tab SHALL retain the sticky `N of M decided` tally and jump segments, and a take-recommendations button selecting only recommended undecided decisions on the selected board. FYIs SHALL render subject/body with the same safe text/link renderer, comment field and exclusive Comment, Dismiss, or Comment and dismiss pending action. Typing alone SHALL NOT queue an FYI action. Comment actions SHALL require non-whitespace notes. One persistent Review queued and one Submit actions control SHALL cover all queued actions across boards and tabs. Submission SHALL require at least one queued action and valid required notes for every queued card, including hidden tabs and unselected boards.


Saved annotations SHALL additionally queue feedback and satisfy comment content as defined by Queued annotation feedback; the original typing-only and answer-note rules remain for unsaved text and answers. FYI Dismiss with saved annotations SHALL be visibly Comment and dismiss. Annotation-only decision comments SHALL use the same global review/submit controls.

#### Scenario: Membership mirrors the rail

- **WHEN** Hera shows visible coordinators, a hidden coordinator, an archived coordinator and plain workers
- **THEN** the page lists exactly the visible coordinators in the rail’s order and no hidden/archived coordinator or worker

#### Scenario: Nested sub-coordinator

- **WHEN** a visible coordinator owns a sub-coordinator plus plain workers
- **THEN** the sub-coordinator is indented beneath its canonical parent and workers are absent

#### Scenario: Greyed empty row

- **WHEN** a coordinator has no open decisions or FYIs, including after its last item is dismissed
- **THEN** its nav row stays in place, greyed with no counts and still selectable, and either tab shows `Nothing open for <name>`

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

Submit SHALL snapshot only explicitly queued answers and FYI actions across all tabs/coordinators, including each item's loaded `expected_updated_at`, and SHALL leave all unqueued items untouched. FYI actions SHALL carry stable action IDs and immutable payloads retained on uncertain outcomes and reload; the page SHALL reconcile an uncertain result before allowing payload edits and SHALL generate a new ID for a revised action after a definitive conflict/invalid result. The page SHALL partition requests by owning role and endpoint in batches of at most 16, prevent concurrent duplicate submits and match every submitted action to a result. When every submitted action succeeds it SHALL finish even if untouched items remain open. Otherwise it SHALL stay open and apply results per card: resolved decisions become read-only Answered; dismissed FYIs become read-only Dismissed; accepted comments remain open but their queued action/draft is cleared and marked sent; conflicts replace current content, preserve note text, clear the pending choice/action and flag the edit; already_resolved/not_found cards are marked closed; invalid cards show an error. A comment-only action against an already-resolved item SHALL need attention rather than count as delivered. Confirmed actions SHALL NOT be resubmitted during retry of failed or unconfirmed work. The page SHALL report accepted action counts and remaining failures, never overall success on a partial failure.


Snapshots and immutable retries SHALL include annotations and required decision response action IDs. Decision comment outcomes SHALL accept commented, clear confirmed feedback and leave the item open. Fresh feedback rejected on closed items SHALL retain drafts for attention; already_resolved SHALL NOT confirm undelivered annotations. Conflicts SHALL keep all original types, ranges, quotes/comments in recoverable stale drafts and require reanchoring or removal before requeue.

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

#### Scenario: One queued board becomes hidden or archived

- **WHEN** one coordinator becomes hidden or archived while its answers and another coordinator’s answers are queued
- **THEN** writes to the unavailable board are refused with a clear message and its drafts are kept, the other coordinator’s valid actions still submit, and the page stays open showing partial results

#### Scenario: Comment response lost

- **WHEN** a comment commits but its response is lost and the page reloads
- **THEN** retry uses the original action ID and confirms the stored outcome without sending another coordinator notice

### Requirement: Live change awareness and drafts

While visible and unfinished the page SHALL poll scoped navigation metadata and mixed-item endpoints every 20 seconds. Changes SHALL produce a Refresh banner without replacing editing cards or drafts. Accepted refresh SHALL reconcile the visible rail tree using the canonical visibility/order policy; server-side visibility revocation SHALL take effect even before refresh. Drafts for removed/hidden coordinators SHALL remain marked unavailable in Review queued. A submit SHALL refuse writes to those boards with a clear hidden/archived/unavailable message while still submitting queued actions for other authorized coordinators. Refused batches SHALL retain drafts and prevent overall success/close, never silently drop the actions. Selection SHALL stay on the same role if still visible, otherwise move to the first non-empty remaining row (or first row when all empty). Drafts SHALL persist in `localStorage` keyed by role, item ID and item revision, containing decision choice/note or FYI pending action/note/action ID; no credentials SHALL be stored there. Explicit refresh after a coordinator revision SHALL discard stale drafts; conflict results SHALL retain note text as described in partial results. Unrevised drafts SHALL survive reload, tab changes and session expiry. While unfinished, Copy responses SHALL copy or expose selectable plain text for all queued decisions/FYIs across tabs, labeled by coordinator and action.


For annotation-bearing drafts, an accepted refresh after revision SHALL retain the prior note/annotations as a recoverable stale draft instead of discarding them. It SHALL clear the queued choice/action and require explicit reanchoring or removal before submit. Frozen uncertain payloads SHALL never be discarded or changed by refresh.

#### Scenario: New item arrives

- **WHEN** an existing scoped coordinator adds a decision or FYI
- **THEN** polling offers Refresh without changing edits; a newly visible coordinator appears in canonical rail order only after accepted refresh

#### Scenario: Revised item discards its draft

- **WHEN** the coordinator revises an item and the user accepts Refresh
- **THEN** that card loses its stale draft while unrevised cards keep theirs

#### Scenario: Copy fallback

- **WHEN** Copy responses is used on an unfinished mixed round
- **THEN** decisions and FYI actions/notes across tabs are copied or selected for manual copying with coordinator labels

### Requirement: Finish successful submissions

The page SHALL attempt `window.close` only after every submitted action across all role batches has a matching accepted result. Accepted decision outcomes SHALL be resolved/already_resolved; accepted FYI comment outcomes SHALL be commented; accepted FYI dismissal/comment-and-dismiss outcomes SHALL be resolved/already_resolved. Replay of a stored successful result SHALL count as accepted. Untouched decisions/FYIs and successfully commented FYIs remaining open SHALL NOT block completion. If closing throws or leaves the page open, the page SHALL replace its action controls with `Actions sent — you can close this tab`, accepted counts for answers/comments/dismissals and a same-origin Return to Argus link. Any conflict, invalid, not_found, missing/mismatched result, unaccepted outcome or failed request SHALL keep the page open with confirmed results and drafts. Completion SHALL stop polling and fence late responses from removed controls.


Accepted decision comment outcomes SHALL additionally be commented. Annotation-bearing actions SHALL require an explicit receipt-confirmed success; already_resolved without delivery confirmation SHALL NOT count as accepted feedback.

#### Scenario: Mixed successful submit

- **WHEN** all submitted answers, comments and dismissals succeed while untouched items and comment-only FYIs remain open
- **THEN** the page attempts to close

#### Scenario: Browser refuses closing

- **WHEN** every submitted action succeeds but closing is refused or throws
- **THEN** the sent confirmation, accepted counts and Return to Argus link replace the action controls

#### Scenario: Submission needs attention

- **WHEN** any role batch fails or any submitted action has an error or missing result
- **THEN** no close attempt or overall sent confirmation occurs and confirmed results and remaining drafts stay available



### Requirement: One summary message to the coordinator

When an answer-route submission resolves at least one decision or accepts at least one new decision comment, the system SHALL write exactly one self-addressed coordinator message atomically with those resolutions, delivered through the board-notice outbox and shown as `Aaron (board)`. It SHALL list resolved decisions in ID order with ID, subject, chosen option label (marking recommendation or Something else), note or none, and that coordinator's remaining open IDs of both kinds. It SHALL state each answer is already resolved and each comment-only decision remains open. A submission accepting neither resolutions nor new comments SHALL write no message and SHALL NOT also write the human-dismissal dropped notice. Full answer notes up to 4 KiB SHALL remain on each item; summaries SHALL fit 64 KiB by evenly budgeting notes and ending shortened notes with `… [truncated — full note on board item #N; hera_board_list include_resolved=true]`. Notes within budget SHALL remain untouched. FYI batches SHALL follow the separate durable FYI comment contract, producing at most one FYI summary per owning coordinator and no message for dismiss-only batches. No summary SHALL include another coordinator's board. All notices SHALL remain pending durably when no binding exists and deliver once a binding exists.

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

The existing summary SHALL include annotation type, quoted selection and comment under each owning item, with no additional annotation messages. General notes and annotation feedback SHALL share the 64 KiB budget; shortened or omitted annotation entries SHALL point to full durable response receipts by role and action ID. Full annotations SHALL persist with those receipts, including on resolved answers.

#### Scenario: Annotations share the response summary

- **WHEN** a role batch accepts annotated answers and annotation-only comments
- **THEN** one existing summary includes each quoted annotation, states which decisions stay open and creates no separate annotation messages


## ADDED Requirements

### Requirement: Text-range annotations on decisions and FYIs

The page SHALL allow any nonempty span of rendered body or authored option text to be selected, including nested list text, option labels/summary/upside/downside/recommendation reason, and ranges spanning paragraphs, bullets, fields, bold/code/link text within one item. Subjects, headings and UI control text SHALL NOT be selectable annotation content; cross-item ranges SHALL be rejected without altering selection or drafts. A range SHALL identify its item revision, stable annotation ID, source-field paths, start/end UTF-16 offsets over rendered text, selected-text hash and up to 64 Unicode code points of prefix/suffix context at each boundary. A canonical ordered text projection SHALL use newlines between source fields and exclude markup, UI labels and highlight wrappers. The server SHALL validate the range against expected_updated_at and derive a quoted preview of at most 512 UTF-8 bytes without splitting characters. Longer selections SHALL retain their full range/hash even when previews shorten. Repeated and overlapping text SHALL remain distinguishable by offsets and IDs.

#### Scenario: Arbitrary and nested text

- **WHEN** Aaron selects part of a sentence, text across bold/link nodes, nested bullets or a span across paragraphs
- **THEN** only the selected range is anchored with offsets/context and formatting and links remain safely rendered

#### Scenario: Option text

- **WHEN** Aaron selects a phrase from option text including its label or recommendation reason
- **THEN** that exact phrase can be annotated without choosing the option

#### Scenario: Excluded boundaries

- **WHEN** a selection includes an item subject, heading, UI control or another item
- **THEN** annotation creation is refused clearly and no incorrect range is saved

#### Scenario: Repeated and long selection

- **WHEN** two identical phrases occur in one item and a selection exceeds its quote preview budget
- **THEN** offset/context/hash identify the intended occurrence and the full range remains recoverable even with a shortened preview

### Requirement: Selection toolbar and typed highlights

A nonempty valid mouse, touch or keyboard selection SHALL expose a small floating viewport-clamped toolbar with Copy, Delete and Comment, matching the installed Plannotator types. Comment SHALL open a quote-and-comment composer and require non-whitespace text before Save. Delete SHALL immediately save a DELETION suggestion with optional empty comment; it SHALL show a distinct red highlight/strikethrough without modifying board content. COMMENT SHALL show a distinct inline highlight. Replace/suggest and replacement payloads SHALL NOT be offered. Cancel/Escape SHALL discard only unsaved editor changes. Overlapping and identical ranges SHALL be allowed as distinct annotation IDs; the latest painted annotation SHALL receive direct highlight activation, and every overlapping annotation SHALL remain independently accessible/removable in the panel. Removing one SHALL preserve all remaining ranges.

#### Scenario: Toolbar and types

- **WHEN** Aaron selects text and activates Comment or Delete
- **THEN** Comment opens a composer and Delete queues a redline suggestion without requiring comment text; no Replace type appears

#### Scenario: Overlapping selections

- **WHEN** Aaron comments on a phrase and suggests deleting an overlapping span
- **THEN** both annotations persist, direct overlap activation selects the newest highlight and removing either preserves the other

#### Scenario: No source edits

- **WHEN** Aaron saves a Delete annotation
- **THEN** the coordinator-authored item body and option data remain unchanged until the coordinator acts on the suggestion

#### Scenario: Cancel and copy

- **WHEN** Aaron copies a selection or cancels a pending Comment
- **THEN** Copy changes no queued response and Cancel preserves existing saved annotations

### Requirement: Annotation sidebar and phone sheet

The page SHALL show an annotations panel for the current item in a desktop right column and a modal bottom sheet/drawer at phone width. Interacting with item content or its Annotations control SHALL select the current item. The panel SHALL list annotations in document order by start offset, end offset and creation order, with type, quoted selection and any comment. Entry activation SHALL scroll to and flash its highlight for two seconds. Highlight activation SHALL select the corresponding entry for editing/removal. Comments SHALL expose Edit, Save, Cancel and Remove; Delete suggestions SHALL expose Remove. The existing note SHALL be labeled General comment and remain the single whole-item feedback/required-answer-note store. It SHALL retain prior answer validation and explicit FYI queue semantics. The sheet SHALL have a labeled title, Close/Escape, focus trap/restoration and controls of at least 40px. Text selection on touch SHALL preserve native long-press/drag handles until the toolbar action captures the range; controls SHALL remain reachable with the on-screen keyboard. Keyboard users SHALL be able to select text normally or invoke Annotate/Enter from a focused passage as a whole-passage selection fallback, then reach the toolbar with Tab. Editing shortcuts SHALL not override text entry. Normal unselected link activation SHALL follow its URL; selection/annotation/highlight gestures SHALL neither follow links nor toggle radio choices, and SHALL not trigger tab swipes.

#### Scenario: Panel and highlight navigation

- **WHEN** Aaron activates a sidebar entry or an inline highlight
- **THEN** the corresponding entry/range becomes active, the passage scrolls into view and flashes without changing the answer

#### Scenario: Phone selection and sheet

- **WHEN** Aaron long-presses a phrase at 390px and opens Comment or the annotations list
- **THEN** native text selection yields the toolbar and the modal sheet/composer remains reachable without horizontal scrolling in either theme

#### Scenario: Keyboard path

- **WHEN** Aaron focuses a passage and invokes Annotate/Enter, then tabs to Comment
- **THEN** a captured selection reaches the labeled composer and Save/Cancel restores sensible focus

#### Scenario: Links and options

- **WHEN** Aaron follows an unselected reference link or selects linked/option text for annotation
- **THEN** only an ordinary link click navigates and annotation gestures never activate links or select options

#### Scenario: General comment

- **WHEN** Aaron writes whole-item feedback in the existing note
- **THEN** it is labeled General comment, uses the existing note field and does not create a second global-comment annotation

### Requirement: Queued annotation feedback

Saving a valid Comment or Delete annotation SHALL queue the item in the existing global response queue; typing in a composer alone SHALL NOT queue feedback. Review queued and Copy responses SHALL list every annotation beneath its owning coordinator/item with type, quoted selection and any comment, and explicitly identify Delete as a suggestion. A decision with annotations and no chosen answer SHALL queue Comment and remain open after acceptance, including a deletion-only response with no note. Option answers SHALL carry annotations; Something else and plain answers SHALL still require their general answer note. On FYIs, the first annotation SHALL default an unset action to Comment, and Dismiss plus annotations SHALL visibly become Comment and dismiss. Removing the last annotation SHALL unqueue an implicit annotation-only comment unless an explicit valid note comment remains. One existing Submit actions SHALL send queued feedback across all boards/tabs.

#### Scenario: Deletion-only feedback

- **WHEN** Aaron saves a Delete suggestion on a structured or plain decision without an answer choice or note
- **THEN** the item queues Comment, acceptance counts as a comment and the decision remains open with unchanged read state/revision

#### Scenario: Answer and required note

- **WHEN** Aaron adds range annotations and chooses an option or Something else
- **THEN** the declared option answer includes all annotations; Something else and plain answers remain invalid without their required note

#### Scenario: FYI action conversion

- **WHEN** Aaron saves any annotation on an unset FYI or on Dismiss
- **THEN** the action becomes Comment or visibly becomes Comment and dismiss respectively

#### Scenario: Review and removal

- **WHEN** Aaron reviews multiple typed annotations then removes the only annotation from an otherwise untouched item
- **THEN** Review and Copy include every type/quote/comment and removal unqueues only the now-empty implicit response

### Requirement: Annotation draft recovery and limits

Annotations SHALL persist in the same guarded localStorage response records as note drafts, bound to role, item and updated_at. Each COMMENT SHALL require non-whitespace text at most 4096 UTF-8 bytes; DELETION SHALL allow an empty comment (any supplied comment SHALL be at most 4096 UTF-8 bytes); at most 20 annotations of all types combined SHALL be saved per item. The UI SHALL retain editor text on validation/storage failure and expose the limit/error. Drafts SHALL survive reload, tab/board changes and expiry. Frozen uncertain payloads SHALL include annotations and action IDs, remain immutable until reconciled, and retry identically. A conflict or accepted refresh changing the item revision SHALL retain notes, old anchors and quotes in a recoverable stale draft, clear the queued choice/action and require explicit reanchoring or removal before resubmission. No stale annotation SHALL be silently discarded or automatically rebound. Accepted actions SHALL clear only their confirmed drafts; failures and visibility refusals SHALL prevent overall finish and retain feedback. All-confirmed annotation-only comments SHALL finish even though items remain open.

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
