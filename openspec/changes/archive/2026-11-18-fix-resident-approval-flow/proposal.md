## Why

On 2026-11-17 it took Aaron 4+ hours and several confusing errors to get one Linear ticket created through the resident approval board. Board decision #562 became impossible to approve after the resident was relaunched by account moves. It stayed open for hours because nothing closes such items. Its text said approval "expires after ten minutes", which misled both Aaron and the resident. Every decide failure returned the same "invalid, stale or already decided request" error, and the resident had no way to withdraw its own dead item. Separately, the "out of usage budget" notice was posted four times as a `decision` with nothing to decide.

## What Changes

- A lapsed approval item is closed automatically, and the resident is told it can re-send the call. An item lapses when the resident is relaunched before the item is used, or when an approved grant passes its retry window unused.
- Re-sending a call with identical content (same task, cwd, tool and canonical input) supersedes older pending items for it.
- The resident can withdraw its own pending approval item with `hera_board_resolve`. Withdrawing can only cancel; it can never grant.
- Decide failures return specific messages: already approved (with the approval time and the retry deadline), already used, denied, lapsed because of a relaunch, expired unused, withdrawn, superseded by #N, or changed since opened. The TUI explains a decided item instead of offering approve or deny again.
- Board items carry `approval_state`. The resolution note records each transition with a timestamp, including when the call actually ran.
- The retry window after approval grows from 10 to 60 minutes. It stays single-use, exact-call and launch-bound.
- The wording in board items, hook replies, decision notices and the resident orientation becomes accurate. It tells the resident how to wait for a decision while Aaron is present.
- Resident system notices become deduplicated FYIs: the usage-budget notice is an `fyi`, and a repeat of an open item with the same subject updates it in place and marks it unread.

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `resident-approvals`: lifecycle closure (lapse, expiry, supersede, withdraw), specific decide errors, visible approval state, and a 60-minute retry window.
- `resident-agent`: account-switch board notices are deduplicated FYIs, and "no usable account" is no longer a decision.

## Impact

`internal/db` (resident approvals, board columns and upsert), `internal/daemon` (resident tick sweep and account notices), `cmd/argus` (hook reply wording), `internal/tui` (approval decide UX), and tests. Schema: two additive columns on `resident_approvals`. No supervisor spawn-surface change.

Non-goals: running deferred calls in the daemon (connector tools run inside Claude Code with its login), changing who can approve, and web/macOS approval controls (still the named follow-up `resident-approval-client-parity`). The account ping-pong that caused the relaunches is the separate follow-up `fix-resident-follow-move-pingpong`.
