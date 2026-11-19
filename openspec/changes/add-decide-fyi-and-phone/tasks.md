## 1. Approval and test contracts

- [ ] 1.1 Obtain Aaron's approval of this change and the three recommended choices in design.md before writing implementation code; update deltas if approval changes the requirements.
- [ ] 1.2 Read relevant testing, messaging, Hera-view and web-remote gotchas; use temporary HOME/database/runner fixtures, never real ~/.argus or the live daemon.
- [ ] 1.3 Add failing DB/outbox tests for comment-only, silent dismiss, combined action atomicity, material-edit conflicts, closed/wrong-kind/resident guards, receipt replay/changed-payload rejection, 4 KiB comments, truncation/full-receipt recovery and deferred exactly-once delivery.
- [ ] 1.4 Add failing API/session tests for aggregate counts/launch, eligibility, immutable role sets, TUI single-role preservation, token-class/route allowlists, spoofed auth tags, one-use/expiry/restart, malformed/trailing bodies, duplicate IDs/items, 16-action bounds and receipt read authorization.

## 2. Durable FYI actions and REST sessions

- [ ] 2.1 Add durable role/action receipts and full-comment storage with payload fingerprints; implement compare-and-set FYI actions and transactionally commit successful receipts, resolutions and at most one owning-role summary, with no notices for dismiss-only work.
- [ ] 2.2 Implement 64 KiB comment summary budgeting and authenticated full receipt retrieval; document receipt pointers and coordinator access through the authenticated API proxy.
- [ ] 2.3 Add aggregate counts/launch and mixed decide-items/FYI action endpoints with bounded decoding and per-action results; preserve existing decision-only read and answer payloads.
- [ ] 2.4 Extend hashed in-memory tickets/sessions to immutable explicit role sets, enforce eligibility on every scoped read/write and preserve TUI one-role launch and absolute lifetimes.
- [ ] 2.5 Log success/failure/conflicts/replay/skips under consistent board/decide prefixes without tokens, bodies or notes; emit resolution events only after commit.

## 3. Mixed page behavior

- [ ] 3.1 Add failing page tests for counted tabs/groups, FYI focus/empty states, required comment validation across hidden tabs, draft/action-ID recovery and unchanged decision/recommendation behavior.
- [ ] 3.2 Implement grouped Decisions/FYIs, safe FYI rendering, comment/dismiss/combined queueing, mixed Copy responses and draft persistence keyed by role/item/revision.
- [ ] 3.3 Add failing submission tests for multi-role/kind partitioning, accepted comments remaining open, missing/mismatched results, network failures after earlier commits, stable-ID retries and no duplicate actions after reload.
- [ ] 3.4 Implement per-action result matching, accepted-state cleanup, failed/unconfirmed retry handling and close only after all submitted actions succeed; include sent counts and Return to Argus when close is refused.
- [ ] 3.5 Preserve 20-second polling/refresh banners without replacing edits or widening scope; test late-response fencing, expiry recovery, new-ticket context replacement, credential storage boundaries, safe links and Light/Dark/System persistence.

## 4. PWA and TUI entry points

- [ ] 4.1 Add failing PWA tests for global Decide badge/count refresh, zero/error states, header-authenticated aggregate launch, same-origin current-context fragment navigation and offline retry feedback.
- [ ] 4.2 Implement the global Decide entry and aggregate badge on app entry/foreground/visible polling cadence; fit and label it at phone width.
- [ ] 4.3 Add failing TUI launch tests and a SimulationScreen smoke test for FYI-only launch/focus, empty board, no selection, API-disabled behavior and unchanged rebind/remote behavior; implement the open-item guard/focus change.
- [ ] 4.4 Bump SW_VERSION from its then-current value for all modified static shell assets; test /decide remains network-only, outside SHELL_ASSETS, with board/count/session responses uncached.
- [ ] 4.5 Record frontend parity explicitly: TUI keeps one-role launch and supports FYIs, web gains the grouped flow, macOS is intentionally excluded by Aaron (no macOS follow-up required for this work).

## 5. Documentation, verification and delivery after approval

- [ ] 5.1 Update README Reference endpoints and board-launch behavior, plus relevant coordinator guidance on comment versus dismissal and full-comment retrieval; leave marketing prose alone. Keep the existing key binding; if implementation adds/removes/rebinds any TUI key, update keymap, generated help assertions and README together.
- [ ] 5.2 Document non-obvious receipt/outbox atomicity, immutable role scope, retry IDs, cross-batch completion, fragment stripping and SW cache invariants in gotchas and update the knowledge index.
- [ ] 5.3 Verify phone-width browser and installed-PWA navigation, all themes, both tabs, multiple coordinators, FYI-only and empty queues, expiry/restart/retry and refused-close fallback; verify DOM-only links and no credential in URLs/logs/localStorage/cache.
- [ ] 5.4 Run touched-package tests, race tests and coverage reporting, aiming for repository touched-package coverage goals; run full make pre-pr clean before opening/updating the implementation PR. Do not run make pre-pr for this spec-only delivery.
- [ ] 5.5 Review the implementation and test effectiveness using hera-review and hera-review-test-adversary; fix blocking issues.
- [ ] 5.6 Archive add-decide-fyi-and-phone within the implementation PR before merge, inspect merged base requirements and run openspec validate --all --strict; keep spec tooling out of CI/Makefile.
