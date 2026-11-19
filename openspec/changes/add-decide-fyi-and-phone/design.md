## Context

The base board-decision-page spec and archived `add-board-decision-page` and `add-decide-theme-and-links` changes define a standalone `/decide` shell, a 90-second single-use fragment ticket, an eight-hour in-memory role-scoped session, durable answer summaries, safe DOM links, Light/Dark/System appearance and close-on-success behavior. FYIs are currently excluded and a phone user cannot initiate this flow from the PWA.

The PWA already has a per-device token. It must use that credential only in authenticated API headers, keeping the standalone page on a narrower board-only session. Recommendations below are proposed defaults pending Aaron's approval, not approval to implement.

## Goals / non-goals

**Goals:** Handle open decisions and FYIs on a phone, retain coordinator ownership, make comment retries safe, preserve the existing TUI ticket path and give every submitted action a verifiable result.

**Non-goals:** macOS app changes or a macOS follow-up (explicitly dropped by Aaron); live push/SSE updates (retain polling); `/decision` skill posting to the board (separate later change); editing coordinator-authored content; changing resident-approval protections.

## Decisions

### Launch and session boundary

Add `POST /api/hera/board/decision-link`, authenticated by master/device only, to snapshot all eligible coordinator role IDs. Eligible means an existing coordinator whose role and orchestrator are neither archived nor nuked; lack of a live binding does not exclude it. Include coordinators with zero open items so new items for those roles can appear during the session. The response is `{path:"/decide",ticket,expires_at}`. The PWA navigates in its current browsing context to the same-origin relative path with `#t=<ticket>`. It never passes a device token to the page, URL, history or fragment. Same-context navigation avoids popup blockers and works in installed PWAs.

Reuse the existing exchange endpoint and single-use 90-second hashed ticket store. Exchange returns `{session,role_ids,expires_at}` and, for a single-role TUI ticket, retains `role_id`. Sessions hold immutable role-ID sets, are hashed in daemon memory and expire eight hours after exchange. New coordinator roles are excluded until the user reopens from the PWA. Each read/write rechecks membership and role eligibility; removed/archived/nuked roles cannot retain access. Neither expiry nor scope silently expands on polling. No wildcard or device-equivalent session is introduced.

Keep the existing role-specific decision-link endpoint and its one-role ticket unchanged. A role ID or focus in the URL is display context, never authorization. The existing decision-only read and answers routes remain available; new `/board/decide-items` and `/board/fyi-actions` routes supply the page's mixed-kind flow. A session can access these and the existing decision/answer routes only for roles in its set, and no other API routes, including count discovery, launch minting or general board mutations.

### Page layout and navigation

Use one global Decisions/FYIs tab pair with aggregate open counts; each tab groups cards by coordinator, labeled with orchestrator plus coordinator name and stable role ID. Each coordinator heading shows its Decisions/FYIs open counts from the same loaded snapshot. Order groups by role ID and cards by item ID. Initial tab is Decisions unless its open count is zero and FYIs exist. A valid focused FYI selects FYIs and scrolls/highlights it; a focused decision selects Decisions. Empty tabs stay visible with zero counts and an empty state.

Keep decision option cards, required plain/other notes, recommendation shortcuts and tally within Decisions. FYI cards show subject/body using the same safe renderer plus a comment box and one exclusive pending action: Comment, Dismiss or Comment and dismiss. Typing alone does not queue an action. Comment actions require non-whitespace text; dismiss needs none. Switching tabs/groups preserves drafts. One Submit actions button sends all explicitly queued actions across both tabs, with a pending count; it cannot silently omit a queued invalid comment.

### FYI persistence, notice and retries

Use a new per-role `POST .../board/fyi-actions` batch, with `action_id`, `item_id`, `expected_updated_at`, `action` and `note`. Store successful action receipts durably, scoped by role and action ID, with a normalized payload fingerprint, outcome and full comment. A duplicate identical action returns its stored result with no write or second notice; reuse with a different payload is invalid. Retain receipts with the board history rather than expiring them while retries might still occur. Read-only receipt replay precedes item staleness/closed checks. Decision answers retain their existing endpoint and resolved-item retry behavior.

Comment-only leaves the item's resolved/read state and `updated_at` unchanged: it is human feedback, not a material edit to the coordinator's text. Its durable receipt stores the full comment for restart/recycle and truncation recovery. Dismiss sets resolved by human, clears resolution choice, bumps `updated_at`, and emits no coordinator message. Comment-and-dismiss does both atomically, recording the note as the resolution note as well as on the receipt. A transaction commits successful receipts, dismissals and at most one FYI summary per role batch to the existing board-notice outbox; rollback commits none. Ordinary item errors do not block other valid actions. Never also invoke the decision dropped-notice path.

Summaries explicitly distinguish still-open comments from already-dismissed FYIs, include full-note receipt references when shortened, and list that coordinator's remaining open IDs of both kinds. Separate answer/FYI batches may produce separate summaries; never combine recipients across coordinators. Outbox delivery retains its existing exactly-once semantics, `Aaron (board)` attribution and deferred delivery without a binding. A silent dismissal's receipt must not itself enqueue a notice.

### Multi-request completion and recovery

Snapshot the full set of queued actions when submitting, disable duplicate concurrent submits and partition by role/kind in batches of at most 16. Give every FYI action a stable generated ID before its first send and persist that ID with its draft until acknowledged. A network failure keeps the same ID and immutable payload for retry, including across reload; editing its payload requires first reconciling that uncertain result. A revised action after a definitive conflict or invalid result gets a new action ID. Confirm a matching response for every submitted action: decision success is resolved/already_resolved; comment success is commented; dismissal success is resolved/already_resolved. Comment on an already-closed item is not successful because no feedback was delivered. An accepted comment remains visibly open if another submitted action fails, but its pending action/draft is cleared to prevent resubmission.

Close only after every submitted action across every batch succeeds, regardless of untouched open items. A failure in a later coordinator cannot undo earlier commits; display their confirmed results and retry only unacknowledged/failed work. A transport failure stays unresolved until an idempotent retry confirms it. On browser/PWA close refusal show `Actions sent — you can close this tab`, counts of accepted answers, comments and dismissals, and a same-origin Return to Argus link. Stop polling and fence late results after completion.

Poll mixed-item reads every 20 seconds while visible, with the existing refresh banner rather than replacing edits. Persist drafts by role/item/revision, including FYI action and ID; keep the existing conflict behavior of retaining note text but clearing selection/action. The Copy answers control becomes Copy responses and includes decisions and FYIs with coordinator labels. An expired phone session offers Return to Argus to relaunch; a TUI session retains the press-`o` instruction. Neither page reads or copies the PWA's device credential. New session context must supersede any old stored session, and credentials remain sessionStorage-only.

### PWA entry, parity and cache

Add a global `Decide` entry alongside Tasks, Projects, + New and Settings; verify the narrow phone navigation can accommodate it without overflow. Its badge is the sum of open decisions and FYIs across eligible coordinators (not unread counts). `GET /api/hera/board/decision-counts` is master/device-only and returns totals plus per-role counts. Refresh on app entry/foreground and the existing visible polling cadence; no new push dependency. Keep entry available at zero so Aaron can see an empty page. A failed badge fetch shows unavailable rather than a misleading zero; a failed launch stays in the PWA with retry feedback.

Frontend evaluation: TUI retains its one-board launch and gains FYI-only launch/focus; web receives global entry and mixed-action UI; macOS is explicitly excluded at Aaron's direction, superseding a follow-up for this work. Update the README Reference endpoints and launch description. No key binding is added, removed or rebound. Preserve the service worker's network-only `/decide` behavior and exclusion from shell assets; API responses containing data or credentials are not cached. Bump `SW_VERSION` when changing the PWA/decide shell assets in the implementation PR, using the then-current version rather than pinning today's value.

## Risks / trade-offs

- A bounded multi-role session can affect more boards than a TUI session → immutable explicit role set, per-request eligibility checks, eight-hour cap and board-only endpoint allowlist.
- A newly created coordinator is absent from an existing phone session → reopening from the PWA takes a fresh snapshot; refresh never widens access.
- Several role batches can partly commit → preserve per-action results and retry IDs; never report full success or close on a missing result.
- Durable comment receipts add storage → comments are bounded to 4 KiB, receipts share board-history retention and provide retry/truncation recovery.
- PWA navigation can be refused offline or closing can fail → explicit launch errors and sent confirmation/return link; do not cache board data as an offline substitute.

## Migration plan

After Aaron approves, implement with failing tests first. Add receipt persistence and API/session contracts before page/PWA wiring. Existing role launch and answers payloads keep working; no legacy data migration or compatibility layer is needed. Daemon restart clears tickets/sessions as today, but receipts and comments survive. Roll back UI/API together if needed; preserved receipts are durable data. Archive this change with the implementation in the same PR before merge, not in this spec-only delivery.

## Open questions

### Question 1 of 3: Should phone users see all coordinators on one page?

- **Option A:** One page grouped by coordinator gives one place to act, but can be long.
- **Option B:** A coordinator picker keeps pages smaller, but requires repeated navigation.
- **Recommendation:** Use one grouped page with global Decisions/FYIs tabs and per-group counts, because Aaron is not tied to one coordinator on his phone.

### Question 2 of 3: Which boards should an opened phone page be allowed to affect?

- **Option A:** A fixed set of eligible coordinator role IDs gives a stable permission boundary, but new coordinators require reopening.
- **Option B:** All present and future coordinators avoids reopening, but silently widens an already-issued credential.
- **Option C:** One session per coordinator narrows each credential, but complicates recovery and grouped submission.
- **Recommendation:** Use one eight-hour session holding a fixed role-ID set; it supports grouped actions without wildcard permissions.

### Question 3 of 3: Where should the phone launch entry live?

- **Option A:** A global Decide entry makes pending work visible from every main view, but needs careful phone-width layout.
- **Option B:** A button inside Projects saves top-level space, but hides pending work when Aaron is viewing Tasks.
- **Recommendation:** Add a global Decide entry with the total open-count badge, because the queue should be reachable without first choosing a project.
