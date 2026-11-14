## Why

The resident agent (Solo) runs on one Claude account chosen by `resident.account`. When that account's usage budget runs out, Solo stops being able to run turns and Aaron has to notice, edit config, and lose Solo's conversation (a changed `resident.account` makes the next start a fresh launch, because the transcript lives in the old account's config dir). Tasks and projects already have a `>` account move that carries the conversation across accounts, but the resident is explicitly refused (`ErrResidentProtected`) and its account is re-read from config on every start, so even a successful move would be silently undone.

## What Changes

- **Manual move:** `>` on the resident row of the Solo tab opens the same account picker tasks use (including "auto") and moves the resident: transcript copy + resume under the target account via the existing kick-not-stop restart. The choice is stored as a runtime account override in the resident's task meta, which beats `resident.account` until `resident.account` itself is edited.
- **Automatic switch:** the daemon's resident manager watches the resident's current-account usage reading. When the current account is depleted (a live window at 100%) and the resident session is idle, it moves the resident to the eligible account with the most headroom, with a confirmation debounce, a cooldown, a depleted-target guard and refuse-when-none-eligible behavior. Gated by a new `[resident] auto_switch_account` setting (default on).
- **Visibility:** every automatic switch (and every refusal because no account is eligible) is logged via uxlog and slog, sent to Solo as an `[argus]` system turn, and posted as an item on the resident coordinator board so Aaron sees it.
- The resident stays refused by the generic task/backend move paths (`task_move_account`, REST, TUI task `>`); only the resident-specific daemon path may move it, and backend (Claude to Codex) moves stay refused.

## Capabilities

### Modified Capabilities

- `resident-agent`: runtime account override and its precedence over `resident.account`; the resident account move; the automatic switch; the `auto_switch_account` setting.
- `resident-tui`: `>` on the resident row opens the account picker (account moves only).

## Impact

- `internal/agent/accountmove.go` (resident-aware move core), new `internal/db/resident_account.go` (override + guard-bypassing persist), `internal/daemon/resident.go` (effective account in `start`, auto-switch pass in `tick`), daemon RPC `ResidentMoveAccount`, `internal/config/resident.go` + db config key + Settings row, `internal/tui` picker wiring for the resident row, gotchas in `resident.md`.
- No supervisor spawn-surface change (no new spawn args; account already rides Start/Kick requests), no protocol bump.
- README Reference: `[resident]` key table and the `>` keybinding note.
