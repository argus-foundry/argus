## 1. Approval and contracts

- [ ] 1.1 Obtain Aaron's approval of this spec and unpublished mockup before production code.
- [x] 1.2 Write failing delta-derived Go/API tests for annotation validation, revision anchors, receipts, comments on decisions, closed items, auth and bounded decoding.
- [ ] 1.3 Write failing page interaction tests for arbitrary/cross-node text ranges, overlaps, types, selection toolbar/panel, links versus selection, keyboard/touch and queue states.

## 2. Durable response model and API

- [x] 2.1 Extend existing receipt/fingerprint model to annotation arrays and decision actions; retain full typed feedback and validated range anchors/context with server-derived quotes.
- [x] 2.2 Extend /answers and /fyi-actions under existing rail visibility/auth, 16-item batch and whole-body validation; enforce 20 annotations, 4096 UTF-8 bytes each and 512-byte quotes.
- [x] 2.3 Implement atomic receipt/resolution/outbox summaries, 64 KiB budgeting and full receipt recovery through the existing privileged read path.
- [x] 2.4 Test restart/unbound delivery, exact replay, changed-payload IDs, partial failures and no duplicate event/notice behavior.

## 3. Shared decide page

- [ ] 3.1 Render a canonical selectable text projection for body/option text; capture revision-bound arbitrary cross-node ranges, offsets/context/hash and independent overlapping IDs.
- [ ] 3.2 Add verified Plannotator Copy/Delete/Comment toolbar, safe inline highlights, Comment Save/Cancel/Edit and Delete removal; preserve native selection, keyboard fallback, radio/link isolation and 40px targets.
- [ ] 3.3 Add current-item desktop annotations panel and modal phone sheet with document ordering, entry-to-highlight scroll/flash and General comment label; queue comment-only decisions and FYI comment actions; carry annotations with answers, review and Copy responses using existing submit.
- [ ] 3.4 Persist guarded annotation drafts and frozen retries; preserve recoverable stale quotes/comments on conflicts and refresh, requiring explicit reanchoring/removal.
- [ ] 3.5 Verify 1200px and 390px light/dark, no horizontal overflow, keyboard, native touch selection, cross-node/nested text ranges, repeated/overlapping selections, deletion-only feedback and option selection isolation; bump SW_VERSION when shell asset changes.

## 4. Guidance, parity and release checks

- [x] 4.1 Update builtin Hera board guidance, board tool descriptions and coordinator spawn/reclaim orientation to address every typed comment/deletion suggestion and retrieve shortened feedback. SPAWN SURFACE CHANGE: update inventory and corresponding contract tests.
- [ ] 4.2 Evaluate all frontends: web implements annotation editing; TUI board editing is a non-goal and its launch uses shared web; retain named native follow-up macos-decide-annotation-parity.
- [ ] 4.3 Update README Reference for receipt recovery/payloads, relevant gotchas and uxlog calls without logging feedback text.
- [ ] 4.4 Run relevant tests, coverage and full make pre-pr; validate all specs strictly and archive into base specs in the eventual implementation PR before merge.
