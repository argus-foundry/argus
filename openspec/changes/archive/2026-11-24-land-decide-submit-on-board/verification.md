# Decide submit verification

- Scope: standalone /decide landing and draft recovery; readiness-only board revisions. TUI/macOS retain their existing interactions and wire schema, and receive the same normal board update event.
- TDD: the new action/todo readiness regression failed before implementation (revision changed; Done conflicted), then passed. Material body edits still conflict; receipt replay is byte-for-byte equivalent and readiness still emits an update and marks unread.
- Browser regression coverage: 64 submit cases across desktop/phone and light/dark, 66 existing Decide cases, 64 annotation cases across Chromium/WebKit and desktop/phone, and 8 real isolated-Go-API cases. No live daemon or production data is mutated by these tests.
- Cases cover full acceptance, unanswered cards in the current tab, other-tab counts, local/remote conflicts, closed/hidden feedback with and without text, annotation recovery, both close outcomes, legacy saved drafts, immutable uncertain retries, one-item counts, explicit grouped review, and refresh failure after acceptance.
- Full make pre-pr passed with GOFLAGS=-p=3 (89.1% filtered coverage; 816 workspace tests). Push and PR creation are gated on a clean repetition against the completed tree.
- OpenSpec strict validation: 94 items passed before archive; repeat after archive before commit.

## Revision investigation

Read and list/reorder paths already preserve updated_at. trigger_pending-only updates previously advanced it, falsely rejecting queued Done on both actions and todos; the regression reproduces this and the server now preserves authored revisions for readiness-only changes. Body, command, trigger text and blocking edits remain material. Receipt fingerprinting and replay are unchanged.

Read-only inspection identified #657 as an action and #737 as a todo, both revised since creation. The live database retains no revision history and the old browser expected_updated_at is unavailable, so the exact historical cause of those two conflicts cannot be proven. The harmless-readiness conflict is independently reproduced and fixed.

## Visual review

Evidence directory: /tmp/argus-decide-after-submit-1795504872298379000/decide-submit/

- 24 before and 32 after PNGs cover 1200px/390px and light/dark. Names are before/after-STATE-WIDTH-THEME.png.
- accepted-none, accepted-same and accepted-other cover the calm confirmation alone or above remaining cards, including other-tab count badges.
- mixed and remote-failure cover per-card failure placement and retained board context.
- recovery-false and recovery-true cover closed/unavailable collapsed notes; recovery-expanded additionally shows retained text, annotation quote/comment, Copy and Discard.
- Reviewed the images directly. Removed the automatic global queue takeover, repeated status text, cumulative footer, duplicate recovery labels and post-submit scroll displacement. Confirmations are quiet and left aligned; phone controls wrap without horizontal overflow. A partially visible lower card at phone width remains scrollable above the persistent footer.
- Screenshots wait for submission completion, avoiding misleading captures of the transient Sending state. No marketing screenshot files are changed.

## Review findings resolved

- Broad review: retained refresh errors after acceptance, normalized legacy saved conflicts into per-card attention, retained undelivered written feedback on already_resolved, and avoided duplicate uncertainty explanations.
- Test-adversary review: corrected a stale mock snapshot that reverted an authored revision, waited for real completion before screenshots, and added independent per-item count, action reconfirmation, event/replay, closed annotation and legacy-draft assertions.
- Remaining actionable findings: none. Historical attribution of #657/#737 remains limited by unavailable revision history.
