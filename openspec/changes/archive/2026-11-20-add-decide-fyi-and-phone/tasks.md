## 1. Approval and test contracts

- [x] 1.1 Obtain Aaron's approval of this change and the revised rail layout and session-scope/entry recommendations in design.md before writing implementation code; update deltas if approval changes the requirements.
- [x] 1.2 Read relevant testing, messaging, Hera-view and web-remote gotchas; use temporary HOME/database/runner fixtures, never real ~/.argus or the live daemon.
- [x] 1.3 Add failing DB/outbox tests for comment-only, silent dismiss, combined action atomicity, material-edit conflicts, closed/wrong-kind/resident guards, receipt replay/changed-payload rejection, 4 KiB comments, truncation/full-receipt recovery and deferred exactly-once delivery.
- [x] 1.4 Add failing API/session tests for aggregate counts/launch, eligibility, dynamic rail visibility, nested/hidden/archived membership, canonical ordering, TUI preselection parity, token-class/route allowlists, spoofed auth tags, one-use/expiry/restart, malformed/trailing bodies, duplicate IDs/items, 16-action bounds and receipt read authorization.

## 2. Durable FYI actions and REST sessions

- [x] 2.1 Add durable role/action receipts and full-comment storage with payload fingerprints; implement compare-and-set FYI actions and transactionally commit successful receipts, resolutions and at most one owning-role summary, with no notices for dismiss-only work.
- [x] 2.2 Implement 64 KiB comment summary budgeting and authenticated full receipt retrieval; document receipt pointers and coordinator access through the authenticated API proxy.
- [x] 2.3 Add aggregate counts/launch and mixed decide-items/FYI action endpoints with bounded decoding and per-action results; preserve existing decision-only read and answer payloads.
- [x] 2.4 Extend hashed in-memory tickets/sessions to the shared rail-board auth class, add scoped navigation metadata, enforce canonical current visibility on every scoped read/write and retain TUI initial selection and absolute lifetimes.
- [x] 2.5 Log success/failure/conflicts/replay/skips under consistent board/decide prefixes without tokens, bodies or notes; emit resolution events only after commit.

## 3. Mixed page behavior

- [x] 3.1 Add failing page tests for selected-board counted tabs, canonical coordinator tree, greyed selectable empties, nested sub-coordinators, TUI/phone initial selection and FYI focus/empty states, required comment validation across hidden tabs, draft/action-ID recovery and unchanged decision/recommendation behavior.
- [x] 3.2 Implement the desktop left coordinator nav and selected-board Decisions/FYIs, safe FYI rendering, comment/dismiss/combined queueing, mixed Copy responses and draft persistence keyed by role/item/revision.
- [x] 3.3 Add failing submission tests for multi-role/kind partitioning, accepted comments remaining open, missing/mismatched results, network failures after earlier commits, stable-ID retries and no duplicate actions after reload.
- [x] 3.4 Implement per-action result matching, accepted-state cleanup, failed/unconfirmed retry handling and close only after all submitted actions succeed; include sent counts and Return to Argus when close is refused.
- [x] 3.5 Preserve 20-second polling/refresh banners with visibility rechecks, accepted nav refresh and unavailable queued drafts without replacing edits; test late-response fencing, expiry recovery, new-ticket context replacement, hidden/archived-board clear refusal while other boards submit, and queued-action recovery, credential storage boundaries, safe links and Light/Dark/System persistence.

- [x] 3.6 Add failing keyboard tests for Cmd/Ctrl arrow traversal including greyed/nested rows, end clamping, tab selection, modifier/IME guards and native editing in input/textarea/select/contenteditable/textbox descendants; implement shortcuts with handled-event default prevention only.
- [x] 3.7 Add failing 390px drawer/tab/swipe tests and implement the same nav tree in a modal phone drawer with focus trap, Escape/Close dismissal, focus restoration and no interception of editable/interactive/vertical gestures.
- [x] 3.8 Share canonical Hera visibility/order/parent logic with navigation projection; test workers excluded, hidden/archived/nuked branches excluded, empty rows retained, nested parent IDs, count-independent placement and kanban-induced rail order. Document omitted group headings/fold controls as the named presentation non-goal.

## 4. PWA and TUI entry points

- [x] 4.1 Add failing PWA tests for global Decide badge/count refresh, zero/error states, header-authenticated aggregate launch, same-origin current-context fragment navigation and offline retry feedback.
- [x] 4.2 Implement the global Decide entry and aggregate badge on app entry/foreground/visible polling cadence; fit and label it at phone width.
- [x] 4.3 Add failing TUI launch tests and a SimulationScreen smoke test for FYI-only focus, empty-board launch, coordinator preselection, no item selection, API-disabled behavior and unchanged rebind/remote behavior; implement common-page launch with initial role/focus.
- [x] 4.4 Bump SW_VERSION from its then-current value for all modified static shell assets; test /decide remains network-only, outside SHELL_ASSETS, with board/count/session responses uncached.
- [x] 4.5 Record frontend parity explicitly: TUI preselects its coordinator on the common page, web gains the same rail/drawer flow, macOS is intentionally excluded by Aaron (named follow-up: no /decide entry point until the Mac app resumes).

## 5. Documentation, verification and delivery after approval

- [x] 5.1 Update README Reference endpoints and board-launch behavior, plus relevant coordinator guidance on comment versus dismissal and full-comment retrieval; leave marketing prose alone. Keep the existing key binding; if implementation adds/removes/rebinds any TUI key, update keymap, generated help assertions and README together.
- [x] 5.2 Document non-obvious receipt/outbox atomicity, current rail visibility authorization, retry IDs, cross-batch completion, fragment stripping and SW cache invariants in gotchas and update the knowledge index.
- [x] 5.3 Verify phone-width browser and installed-PWA navigation, all themes, both tabs, multiple coordinators, FYI-only and empty queues, expiry/restart/retry and refused-close fallback; verify DOM-only links and no credential in URLs/logs/localStorage/cache.
- [x] 5.4 Run touched-package tests, race tests and coverage reporting, aiming for repository touched-package coverage goals; run full make pre-pr clean before opening/updating the implementation PR. Do not run make pre-pr for this spec-only delivery.
- [x] 5.5 Review the implementation and test effectiveness using hera-review and hera-review-test-adversary; fix blocking issues.
- [x] 5.6 Archive add-decide-fyi-and-phone within the implementation PR before merge, inspect merged base requirements and run openspec validate --all --strict; keep spec tooling out of CI/Makefile.

## Verification evidence

- One full `make pre-pr` run completed all packages. Its spawn-surface fingerprint failure was corrected with spawn surface 56 and a passing targeted surface/coherence recheck; an unrelated MCP host-busy timeout passed its exact isolated race-test rerun. The full suite was not repeated, honoring the shared-machine test budget.

- Build, vet, formatting, final staged-diff lint, vulnerability scan, workspace tests and the coverage filter passed; filtered full-suite coverage is 89.2% against the 88% floor.

- Browser evidence: 46 Chromium desktop/phone checks passed before the last recovery edge was added, the final 12 Chromium rail checks pass, and all 24 final WebKit phone checks pass. The actual PWA shell is exercised with isolated intercepted APIs; no live daemon or real ~/.argus data is touched.

- The named macOS follow-up is no /decide entry point until the Mac app resumes. No merge or deployment is performed by this worker.
