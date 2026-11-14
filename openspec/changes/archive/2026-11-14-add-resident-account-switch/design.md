## Context

- `agent.MoveTaskAccount` copies the Claude transcript into the target account's config dir, persists the account (`SetTaskAccountMove`), and restarts a live session in place with `KickRerender` (resume). It refuses the resident twice: an explicit `IsResident()` check and `residentGuard` inside `setTaskAccountLocked`.
- `residentManager.start` sets `task.Account = cfg.Resident.Account` on every start and resumes `task.SessionID`. Today, changing `resident.account` therefore loses the conversation.
- Per-account usage comes from `usagebudget.CachedReadingForAccount` (bridge + probe, newest window wins); `LiveWindows` drops windows that are reset or older than `CacheMaxAge`. Auto move targets use `pickMoveTargetWhere` (lowest `Percentage`, unknown counts as 0).
- A rate-limited Claude session cannot run turns, so Solo cannot call a tool to rescue itself. The trigger must be daemon-side.

## Decisions

### D1: `resident.accounts` is an ordered preference list; the current account is a persisted marker

`resident.accounts` (config.toml `[resident] accounts = [...]`, DB key `resident.accounts` stored as a JSON array) replaces `resident.account`. Entries are Claude account names, `default`, or `auto` (any Claude-capable account by most headroom; allowed alone or last). Unset means `["default"]`. A single legacy DB value `resident.account` is read as a one-element list when `resident.accounts` is absent (this is how Aaron's hand-set `personal` survives); nothing else of the old setting remains (config.toml `account` is simply ignored as an unknown key, Settings and `account_references` drop it).

One pure function in `internal/agent` (`ResidentAccountPlan`) normalizes the list (skip unknown/Codex-only/duplicate entries with a log, cut at the first `auto`) and answers: which account does the list want right now (first non-depleted entry; `auto` picks via `pickMoveTargetWhere` with depleted filtered), and the preference rank of any account. Start, the auto switch and the Settings editor all use it.

The live account is where the resident currently is: the row's `account` plus a runtime marker `account_override` in task meta `{account, by manual|auto, at}` written in the same transaction as the row on every resident move. On start: keep the persisted account if allowed by the list and not depleted (a `manual` marker is also kept when out of the list, if it exists, supports Claude and is not depleted); otherwise pick per the list; if every listed account is depleted keep the persisted account (or, on a first start, the first usable listed entry). The marker never edits `resident.accounts`.

Rejected: rewriting the setting on a move (config.toml wins, and it would turn preference into history); keeping the single `resident.account` plus an override with a `base` check (superseded by the list).

### D2: Resident account move is a daemon-side path, not the generic move

Add `agent.MoveResidentAccount`, sharing the existing move core (target validation, `claimAccountMove`, transcript transfer, kick-not-stop). The core takes an `allowResident` mode, off for every existing entry point, so `task_move_account`, REST and the TUI task picker still refuse the resident (backend/Codex moves stay refused too: `MoveTaskBackend` unchanged).

Persist order: (1) transcript copy to the target account, (2) one transaction writing the row's account and session id plus the override meta (new `db.SetResidentAccountMove`, the only guard-bypassing writer, resident row only), (3) restart. Same persist-before-kick as tasks, so a crash between 2 and 3 leaves the next ensure-loop start resuming under the new account. A transfer with no transcript (fresh resident) still switches the account and starts fresh.

Entry points run in the daemon so they serialize with the ensure loop: the TUI calls a new daemon RPC `Daemon.ResidentMoveAccount{account, force}`; the auto-switch calls the same function. The in-flight claim plus `HasPendingRestart` already keeps the ensure loop from racing the kick (`tick` returns while a restart is pending). The manual path refuses a busy session unless forced, like tasks.

The resident launches with its own private launch dir and per-launch files (`resident-memory.md`); the implementation must confirm they resolve per effective account (they already must for a configured non-default `resident.account`) and test a move default to named and back.

### D3: Automatic trigger lives in the resident manager tick

In `residentManager.tick`, in the branch where the resident session is alive, call `maybeAutoSwitch`. It acts only when ALL hold:

1. `cfg.Resident.AutoSwitchAccount` is on (default true).
2. `ResidentAccountPlan` wants a different account than the current one for an allowed reason: (a) the current account's reading, through `usagebudget.LiveWindows`, has a live window with `Percentage >= 100` (depleted); (b) "move back up": the current account is allowed, not marked `manual`, and a more preferred listed entry is no longer depleted; (c) the current account is no longer allowed by the list and is not marked `manual`. The depleted test: This is the existing "depleted" meaning (Codex routing treats depleted as 100%; headroom routing treats over-threshold as no headroom). No reading, stale reading or an unparseable reading never triggers (fail open: unknown is not depleted). No new threshold is introduced.
3. The session is parked: raw-idle, or content-idle through `agent.ContentIdleTracker` (stable emulated screen, no working affordance). The content path exists because Claude Code's usage-limit wait footer ("Continuing automatically at ... Esc to cancel", seen on a live weekly-limit repro) can keep repainting so raw output never goes quiet; a working screen ("esc to interrupt") never counts. A running turn is never interrupted. A needs-input state does NOT block (approved tweak): the usage-limit menu is itself a prompt and the two cannot be told apart cheaply; preferring the switch loses only the pending prompt since the transcript resumes.
4. No move/recycle/restart/graduation is in flight (`AccountMoveInFlight`, `HasPendingRestart`, `m.recycling`).
5. Conditions 2 to 4 hold on two consecutive ticks (`residentSwitchConfirmTicks = 2`, about 10s) so a transient reading or a brief idle blip cannot trigger it.
6. Cooldown: the last automatic switch was at least `residentSwitchCooldown` (10 min) ago. The last switch time is persisted in resident meta (`last_auto_switch_at`) so a daemon bounce does not reset it.

Target: the plan's choice (first non-depleted listed entry, `auto` by most headroom), passed to the move as an explicit account name. If the current account is depleted and none qualifies the manager does nothing except log once per distinct situation and post one board item (D5) per episode ("no eligible account"), then keeps retrying each tick quietly (the cooldown only starts after an actual switch).

The move uses the D2 function with `Force: true`, passed only after the parked check above (the move's own busy check is raw-idle only and would otherwise refuse the repainting limit-wait screen). Because it runs only on a parked session, no turn is lost; the transcript is carried over, so Solo resumes the same conversation on the new account.

Known limitation: depletion is detected from the cached usage reading (bridge reading when the bridge is live, else the periodic probe), so a hard limit can be noticed up to one probe interval late when the bridge is absent. Reading the on-screen limit banner is a named follow-up; it was not added now because it needs a new screen-scrape signal with its own false-positive surface.

### D4: Self-service is NOT in this change (recommendation)

A `resident_switch_account` MCP tool would have to restart the very session that called it; `task_move_account` already forbids self-moves for that reason. A safe version needs a deferred-intent flag the daemon executes at the next idle (like self-service recycle). Marginal value over D3 is small: Solo has no reliable view of its own budget, and the failure case (limit already hit) is handled by the daemon. Recommended follow-up `add-resident-self-switch`: self-only for the verified resident (honoring the identity guard), target `auto` or a named eligible account, writing an intent that this same daemon path executes. Not built here.

### D5: Telling Aaron and Solo

Every automatic switch:

- `residentLog` (uxlog) and `slog.Info` with from, to, readings, resumed (the daemon never calls `uxlog.Init`, so both are required).
- A reliable `ResidentSend` system turn beginning `[argus] ` (so last-exchange extraction skips it): "You were switched from <from> to <to> because <from> hit its usage limit. Your conversation carried over. Continue whatever you were doing."
- A board item on the resident coordinator's human board (kind `fyi`, subject "Solo switched accounts: <from> to <to>", body with usage numbers and time). The board already auto-shows unread items in the resident tab. A refusal for lack of an eligible account posts a board item of kind `decision`.

Manual moves log the same way and show a status-bar notice in the TUI; no board item (the human just did it).

### D6: Setting

`[resident] auto_switch_account` (bool, default true), config.toml and DB key like the other `[resident]` keys, with a Settings -> Resident toggle. With it off the account list only decides the account at start. `[resident] accounts` is edited in Settings through an ordered multi-select modal (D7). config.toml wins over Settings (existing resident behavior). The manual `>` needs no setting.

### D7: TUI

In the resident tab, `>` on the resident's own row calls the existing `openAccountPicker`, parameterized so it lists only Claude accounts plus "auto" (no handoff/Codex rows, since backend moves are refused) and calls `ResidentMoveAccount` instead of the in-process task move. `>` on a worker row stays inert (new `ResidentOwnRowOnly` policy in the keymap data). `resident-tui`'s "move is inert" language is about Hera re-parenting, not this account move. In `--remote` mode the resident tab does not exist, so there is nothing to wire.

## Risks

- Move vs ensure loop vs recycle races: mitigated by `claimAccountMove`, `HasPendingRestart`, the recycle reservation check, and persist-before-kick.
- A mid-restart crash is covered: next `start` reads the marker and resumes under the new account.
- Accounts deleted while current: the next start picks per the list with a log line.

### D8: Settings editor

Settings -> Resident -> Accounts is one row (the ordered list, `(config.toml)` when config.toml defines it). Enter opens a modal: every Claude-capable account plus `<auto>`, checkbox + position + 5h/weekly usage when known (from `agent.AccountUsage`, cached, no probe). Space toggles, `K`/`J` reorder the checked entry, `<auto>` always sorts last, Enter saves (empty selection clears the key so the default list applies), Esc cancels. Modal-local literal keys, like the account picker; they are not rebindable keymap actions, so no keymap/help rows change. Local mode only.
