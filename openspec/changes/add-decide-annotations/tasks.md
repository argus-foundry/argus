## 1. Approval and contracts

- [ ] 1.1 Obtain Aaron's approval of this spec and unpublished mockup before production code.
- [ ] 1.2 Write failing delta-derived Go/API tests for annotation validation, revision anchors, receipts, comments on decisions, closed items, auth and bounded decoding.
- [ ] 1.3 Write failing page interaction tests for body/nested units, option fields, links versus annotation clicks, keyboard/touch and queue states.

## 2. Durable response model and API

- [ ] 2.1 Extend existing receipt/fingerprint model to annotation arrays and decision actions; retain full feedback with server-derived quotes.
- [ ] 2.2 Extend /answers and /fyi-actions under existing rail visibility/auth, 16-item batch and whole-body validation; enforce 20 comments, 4096 UTF-8 bytes each and 512-byte quotes.
- [ ] 2.3 Implement atomic receipt/resolution/outbox summaries, 64 KiB budgeting and full receipt recovery through the existing privileged read path.
- [ ] 2.4 Test restart/unbound delivery, exact replay, changed-payload IDs, partial failures and no duplicate event/notice behavior.

## 3. Shared decide page

- [ ] 3.1 Render revision-local annotatable paragraphs/list lines including nested lines and structured summary/upside/downside controls outside radio labels.
- [ ] 3.2 Add inline Save/Cancel/Edit/Remove, markers, focus restoration, safe link routing and 40px phone affordances.
- [ ] 3.3 Queue comment-only decisions and FYI comment actions; carry annotations with answers, review and Copy responses using existing submit.
- [ ] 3.4 Persist guarded annotation drafts and frozen retries; preserve recoverable stale quotes/comments on conflicts and refresh, requiring explicit reanchoring/removal.
- [ ] 3.5 Verify 1200px and 390px light/dark, no horizontal overflow, keyboard, touch, nested blocks, repeated text and option selection isolation; bump SW_VERSION when shell asset changes.

## 4. Guidance, parity and release checks

- [ ] 4.1 Update builtin Hera board guidance, board tool descriptions and coordinator spawn/reclaim orientation to address every annotation and retrieve shortened feedback. SPAWN SURFACE CHANGE: update inventory and corresponding contract tests.
- [ ] 4.2 Evaluate all frontends: web implements annotation editing; TUI board editing is a non-goal and its launch uses shared web; retain named native follow-up macos-decide-annotation-parity.
- [ ] 4.3 Update README Reference for receipt recovery/payloads, relevant gotchas and uxlog calls without logging feedback text.
- [ ] 4.4 Run relevant tests, coverage and full make pre-pr; validate all specs strictly and archive into base specs in the eventual implementation PR before merge.
