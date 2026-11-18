## 1. Storage and lifecycle

- [ ] 1.1 Add `call_key` and `decided_at` columns to `resident_approvals` (idempotent ALTER), expose `approval_state` through `boardColumns`
- [ ] 1.2 `ApprovalStateError` with per-state messages (matches `ErrResidentApproval` via `errors.Is`)
- [ ] 1.3 `RequestResidentApproval`: compute `call_key`, supersede older pending rows, report reused/superseded, record consume time in the board note, 60-minute window
- [ ] 1.4 `DecideResidentApproval`: specific errors, decision note with time, accurate notice wording
- [ ] 1.5 `SweepResidentApprovals(currentLaunch, now)`: stale/expired transitions, board resolution, one `[argus]` notice each
- [ ] 1.6 Agent withdraw via `resolveHeraBoardItem`; human generic dismissal stays refused with the state message
- [ ] 1.7 `PostOrRefreshHeraBoardItem` upsert for system notices

## 2. Daemon, hook and TUI

- [ ] 2.1 Run the sweep on daemon start and every resident tick
- [ ] 2.2 Resident account notices use the upsert; "no usable account" becomes `fyi`
- [ ] 2.3 Hook reply wording: already queued / replaces #N / how to wait / how to withdraw
- [ ] 2.4 Orientation DEFERRED APPROVALS paragraph matches the new rules
- [ ] 2.5 TUI: no confirm dialog for a non-pending approval item; show the state message; hint line shows the state

## 3. Tests and docs

- [ ] 3.1 DB tests: expiry and lapse sweep (once only), supersede on re-queue, re-click messages per state, board state after approve and deny, withdraw
- [ ] 3.2 Hook, daemon and TUI tests for the new wording and behavior
- [ ] 3.3 Gotchas entry in `context/knowledge/gotchas/resident.md`; `make pre-pr`
- [ ] 3.4 Archive the change into base specs in this PR
