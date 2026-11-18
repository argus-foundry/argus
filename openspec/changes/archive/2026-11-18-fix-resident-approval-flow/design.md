## Context

Diagnosis of the 2026-11-17 incident (resident role 3246, board items 561/562/574, inbox message 29776) comes from the live database, `daemon.log` and the resident transcript.

- **#562 was launch-invalidated, not timed out.** Pending rows have no expiry. `DecideResidentApproval` refuses approval when the row's launch differs from the current launch file, and every relaunch rotates it (account moves at 15:51, 16:07, 16:52 and 17:04). No code ever closed such a row.
- **The board and inbox were not delayed.** `DecideResidentApproval` resolves the board item and inserts the notice in one transaction, and `HeraInbox` has no gating. The resident's `hera_board_list` (17:07:04.9) and `hera_inbox` (17:06:59) both ran before the approval committed (17:07:05.6).
- **Aaron's "invalid, stale" errors at 17:06 came from #562.** #574 was approved successfully afterward. The single `ErrResidentApproval` message could not say so.
- **The agent cannot close its own item.** `resolveHeraBoardItem` rejects every approval item.
- **Repeated notices.** The none-usable episode flag resets after each successful move, so each new depletion episode posted another `decision`.

## Goals / Non-Goals

**Goals:** no approval item stays open after it can no longer do anything; every decide attempt explains the real state; the resident can clean up its own items; one approval is enough when the resident is busy.

**Non-Goals:** the daemon running the call itself; any change to who may approve; web/macOS controls; the account ping-pong (separate change).

## Decisions

- **States.** `pending → approved → consumed`, plus terminal `denied`, `stale` (launch changed before use), `expired` (approved, window passed unused), `withdrawn` (agent cancelled) and `superseded` (identical call re-queued). Only `approved` authorizes, only on its launch, and only before `expires_at`.
- **New columns.** `call_key` is a hash of the identity without launch and session, so identical content on a new launch or session matches. `decided_at` is the time of the last transition. Older rows get `call_key=''`: they cannot be superseded, but they still lapse by launch.
- **Sweep, not triggers.** `SweepResidentApprovals(currentLaunch, now)` runs on every resident manager tick (5s) and once at daemon start. It marks pending or approved rows from another launch `stale`, and approved rows past `expires_at` `expired`. In the same transaction it updates the board item (resolved, note, `updated_at`) and queues one `[argus]` notice per lapsed row with the stored canonical call. An unreadable launch file only skips the launch comparison. Time expiry still runs.
- **Supersede inside RequestResidentApproval.** A new pending row closes older `pending` rows with the same `call_key` as `superseded` (note "superseded by #N"). No notice is sent, because the resident caused it and the hook reply names the replaced items. Approved grants are never superseded.
- **Withdraw through the existing board tool.** An agent resolution of an approval item cancels a `pending` row (`withdrawn`). Any other state returns the state-specific error. A human cannot dismiss through the generic path; approve or deny stays the only human route. This never creates a grant.
- **Typed errors.** `ApprovalStateError` carries a human-readable message and matches `ErrResidentApproval` through `errors.Is`, so existing checks keep working. Times are shown in local `15:04` form (date added when not today).
- **Visible state.** `boardColumns` exposes `approval_state` (empty for non-approval items), and `ResidentApproval` derives from it. The TUI decide handler shows the state message instead of opening the confirm dialog when the item is no longer pending.
- **Window.** 60 minutes from approval (`ResidentApprovalWindow`).
- **Notice dedupe.** `PostOrRefreshHeraBoardItem` updates an open, non-approval item with the same role, kind and subject (new body, `updated_at`, unread) or posts a new one. Resident account notices use it, and "no usable account" becomes an `fyi`.

## Risks / Trade-offs

- A longer window means a slightly longer gap between review and execution. It stays exact-input, single-use and launch-bound.
- After `/clear` in the same launch the resident has no memory of pending items. Approval and lapse notices still reach it, and re-sending the call returns its current status. Live `/clear` detection is not added here. The hook still binds the launch session, so a post-`/clear` permission request may be refused as a hook failure. That needs live verification and is recorded as a follow-up, not changed here.

## Migration Plan

Idempotent `ALTER TABLE` adds the two columns. On the first sweep after deploy, every row from an older launch (#562 today) lapses and is announced once. No spawn surface changes, so restarting the daemon is enough.
