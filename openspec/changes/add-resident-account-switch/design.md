## Context

- `agent.MoveTaskAccount` copies the Claude transcript into the target account's config dir, persists the account (`SetTaskAccountMove`), and restarts a live session in place with `KickRerender` (resume). It refuses the resident twice: an explicit `IsResident()` check and `residentGuard` inside `setTaskAccountLocked`.
- `residentManager.start` sets `task.Account = cfg.Resident.Account` on every start and resumes `task.SessionID`. Today, changing `resident.account` therefore loses the conversation.
- Per-account usage comes from `usagebudget.CachedReadingForAccount` (bridge + probe, newest window wins); `LiveWindows` drops windows that are reset or older than `CacheMaxAge`. Auto move targets use `pickMoveTargetWhere` (lowest `Percentage`, unknown counts as 0).
- A rate-limited Claude session cannot run turns, so Solo cannot call a tool to rescue itself. The trigger must be daemon-side.

## Decisions

### D1: A runtime account override in resident task meta wins over `resident.account`

Store `account_override` in the resident's task_meta (namespace `resident`) as JSON `{account, base, by, at}`: `account` the account moved to, `base` the effective `resident.account` value at the time of the move, `by` `manual` or `auto`, `at` a timestamp.

Effective account for a start = override.account when an override exists and is valid, else `cfg.Resident.Account`. An override is dropped (and logged) when:

- `cfg.Resident.Account != override.base` (Aaron edited the setting after the move, which is newer intent), or
- the account no longer exists / no longer supports Claude (`ValidateAccount`, `AccountSupports`).

Rationale: a plain "move rewrites the setting" would fight config.toml-wins (a key in config.toml masks Settings and the move could not persist) and would make `resident.account` mean "last account used" instead of "preferred account". An override in the DB is invisible to config.toml, survives crash restarts and daemon bounces (it is read on every `start`), and self-clears on a deliberate settings edit, so there is no "clear override" UI. After an automatic switch the resident does NOT move back when the original account recovers (avoids churn); the next depletion triggers the next switch.

Alternatives rejected: rewriting the setting (config.toml masks it; loses the "preferred" meaning); a separate sticky flag that needs a manual clear (a hidden state the user must know about).

### D2: Resident account move is a daemon-side path, not the generic move

Add `agent.MoveResidentAccount`, sharing the existing move core (target validation, `claimAccountMove`, transcript transfer, kick-not-stop). The core takes an `allowResident` mode, off for every existing entry point, so `task_move_account`, REST and the TUI task picker still refuse the resident (backend/Codex moves stay refused too: `MoveTaskBackend` unchanged).

Persist order: (1) transcript copy to the target account, (2) one transaction writing the row's account and session id plus the override meta (new `db.SetResidentAccountMove`, the only guard-bypassing writer, resident row only), (3) restart. Same persist-before-kick as tasks, so a crash between 2 and 3 leaves the next ensure-loop start resuming under the new account. A transfer with no transcript (fresh resident) still switches the account and starts fresh.

Entry points run in the daemon so they serialize with the ensure loop: the TUI calls a new daemon RPC `Daemon.ResidentMoveAccount{account, force}`; the auto-switch calls the same function. The in-flight claim plus `HasPendingRestart` already keeps the ensure loop from racing the kick (`tick` returns while a restart is pending). The manual path refuses a busy session unless forced, like tasks.

The resident launches with its own private launch dir and per-launch files (`resident-memory.md`); the implementation must confirm they resolve per effective account (they already must for a configured non-default `resident.account`) and test a move default to named and back.

### D3: Automatic trigger lives in the resident manager tick

In `residentManager.tick`, in the branch where the resident session is alive, call `maybeAutoSwitch`. It acts only when ALL hold:

1. `cfg.Resident.AutoSwitchAccount` is on (default true).
2. The current account's reading, through `usagebudget.LiveWindows`, has a live window with `Percentage >= 100`. This is the existing "depleted" meaning (Codex routing treats depleted as 100%; headroom routing treats over-threshold as no headroom). No reading, stale reading or an unparseable reading never triggers (fail open: unknown is not depleted). No new threshold is introduced.
3. The session is idle by the raw `IsIdle` signal and the resident is not Waiting on a confirmation (needs-input), so a turn or a pending prompt is never interrupted.
4. No move/recycle/restart/graduation is in flight (`AccountMoveInFlight`, `HasPendingRestart`, `m.recycling`).
5. Conditions 2 to 4 hold on two consecutive ticks (`residentSwitchConfirmTicks = 2`, about 10s) so a transient reading or a brief idle blip cannot trigger it.
6. Cooldown: the last automatic switch was at least `residentSwitchCooldown` (30 min) ago. The last switch time is persisted in resident meta (`last_auto_switch_at`) so a daemon bounce does not reset it.

Target: `pickMoveTargetWhere` over Claude-capable accounts excluding the current one, with a filter that rejects accounts whose live reading is depleted. Lowest usage wins (equivalently most headroom); unknown counts as 0, matching manual auto. If none qualifies the manager does nothing except log once per distinct situation and post one board item (D5) per episode ("no eligible account"), then keeps retrying each tick quietly (the cooldown only starts after an actual switch).

The move uses the D2 function with `Force: false`. Because it runs only on idle, no turn is lost; the transcript is carried over, so Solo resumes the same conversation on the new account.

Known limitation: depletion is detected from the cached usage reading (bridge reading when the bridge is live, else the periodic probe), so a hard limit can be noticed up to one probe interval late when the bridge is absent. Reading the on-screen limit banner is a named follow-up; it was not added now because it needs a new screen-scrape signal with its own false-positive surface.

### D4: Self-service is NOT in this change (recommendation)

A `resident_switch_account` MCP tool would have to restart the very session that called it; `task_move_account` already forbids self-moves for that reason. A safe version needs a deferred-intent flag the daemon executes at the next idle (like self-service recycle). Marginal value over D3 is small: Solo has no reliable view of its own budget, and the failure case (limit already hit) is handled by the daemon. Recommended follow-up `add-resident-self-switch`: self-only for the verified resident (honoring the identity guard), target `auto` or a named eligible account, writing an intent that this same daemon path executes. Not built here.

### D5: Telling Aaron and Solo

Every automatic switch:

- `residentLog` (uxlog) and `slog.Info` with from, to, readings, resumed (the daemon never calls `uxlog.Init`, so both are required).
- A reliable `ResidentSend` system turn beginning `[argus] ` (so last-exchange extraction skips it): "Switched from <from> to <to>: <from> hit its usage limit. Your conversation carried over."
- A board item on the resident coordinator's human board (kind `info`, subject "Solo switched accounts: <from> to <to>", body with usage numbers and time). The board already auto-shows unread items in the resident tab. A refusal for lack of an eligible account posts a board item of kind `blocked`.

Manual moves log the same way and show a status-bar notice in the TUI; no board item (the human just did it).

### D6: Setting

`[resident] auto_switch_account` (bool, default true), config.toml and DB key like the other `[resident]` keys, with a Settings -> Resident toggle. config.toml wins over Settings (existing resident behavior). The manual `>` needs no setting.

### D7: TUI

In the resident tab, `>` on the resident's own row calls the existing `openAccountPicker`, parameterized so it lists only Claude accounts plus "auto" (no handoff/Codex rows, since backend moves are refused) and calls `ResidentMoveAccount` instead of the in-process task move. `>` on a worker row is unchanged. `resident-tui`'s "move is inert" language is about Hera re-parenting, not this account move. In `--remote` mode the resident tab does not exist, so there is nothing to wire.

## Risks

- Move vs ensure loop vs recycle races: mitigated by `claimAccountMove`, `HasPendingRestart`, the recycle reservation check, and persist-before-kick.
- A mid-restart crash is covered: next `start` reads the override and resumes under the new account.
- Accounts deleted while overridden: override dropped at start with a log line.
