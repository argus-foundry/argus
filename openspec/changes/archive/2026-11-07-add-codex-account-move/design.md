# Design

- **Registry, not backend checks.** `Family.TransferSession` is the single source of truth for "can move accounts"; `/api/backends` derives `account_move` from it, so a family advertises support only once it ships a tested transfer.
- **Codex transfer copies, never moves.** The thread row (allowlisted columns only: no `creator_*`, folder or project links) and the rollout file land under the target home's `sessions/` at the same relative path; the source row, rollout and `auth.json` are untouched. Credentials are never read.
- **Verify before mutate.** Target home resolution, same-home check, `codex login status` (an unprobeable status is advisory and does not block) and a present `state_5.sqlite` with a `threads` table are all checked before the account is persisted. A target that has never run Codex has no state DB, so the move is rejected with 400 rather than silently starting a fresh conversation.
- **Missing source thread/rollout** moves the account without resuming (`resumed:false`, fresh start), mirroring Claude's missing-transcript behavior; the source binding is untouched.
- **Index failure** removes only the rollout copy this move created.
- Persist-before-kick, kick-not-stop and rollback-on-failed-kick are unchanged.
- **Deviation:** the partial-outcome body reuses the runtime endpoint's `restart_failed` shape and adds `account` and `moved`, so it also carries `settings_applied`, `live`, `rolled_back`, `revision`, `detail`. A kick that fails while the old session still runs rolls the account back and is reported with `moved:false`, `rolled_back:true`.
- **Deviation:** the closed-out guard runs before target validation, so a closed-out task can get 409 `hera_closeout` ahead of a 400 for a bad target. The request gains an optional `force_revive` for it.
- The MCP `task_move_account` path takes the same per-task lock; the lock is non-reentrant, so the HTTP handler locks once and calls the unlocked core.
