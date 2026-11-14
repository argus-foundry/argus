## ADDED Requirements

### Requirement: The resident can move between Claude accounts and keeps its conversation

The system SHALL let the resident move to another Claude account through a daemon-side path only (RPC `Daemon.ResidentMoveAccount`), accepting a configured account name, `default`, or `auto` (the eligible other account with the lowest current usage). The move SHALL copy the resident's saved conversation into the target account's storage, persist the new account, and restart a live session in place with resume, with no model turn. The persist step SHALL write the resident row's account and session id and the account override in one transaction before the restart. A busy session SHALL be refused unless the caller forces it. The generic task move paths (`task_move_account`, REST and the task `>` picker) and every backend (Claude to Codex) move SHALL continue to refuse the resident. A resident with no saved conversation SHALL still switch accounts and start fresh.

#### Scenario: Manual move carries the conversation

- **WHEN** the resident is idle on account `work` with a saved conversation and the human moves it to `personal`
- **THEN** the transcript is copied to `personal`, the row and override record `personal`, and the session restarts in place resuming the same conversation

#### Scenario: Generic move still refuses the resident

- **WHEN** `task_move_account` or the REST move names the resident's task id
- **THEN** it is refused with the resident-protected error and nothing is copied

#### Scenario: Busy session refused

- **WHEN** the resident is mid-turn and the move is not forced
- **THEN** the move is refused as busy and nothing changes

### Requirement: A runtime account override outranks resident.account until the setting changes

The system SHALL store the account chosen by a resident move (manual or automatic) as a runtime override in the resident's task meta, recording the account, the effective `resident.account` value at that time (`base`), who chose it (`manual` or `auto`) and when. The ensure loop SHALL use the override's account in place of `cfg.Resident.Account` on every start, so crash restarts and daemon bounces keep the moved account. The override SHALL be dropped, with a log line, when `cfg.Resident.Account` no longer equals its `base`, or when its account no longer exists or no longer supports Claude. Neither a move nor the override SHALL modify `resident.account`, config.toml or the Settings value.

#### Scenario: Move survives a crash restart

- **WHEN** the resident was moved to `personal` and its session later crashes
- **THEN** the ensure loop restarts it on `personal` and resumes the conversation

#### Scenario: Editing the setting clears the override

- **WHEN** the resident was moved to `personal` (base `work`) and the human then sets `resident.account` to `other`
- **THEN** the next start drops the override and uses `other`

#### Scenario: Deleted account drops the override

- **WHEN** the override names an account that has been deleted
- **THEN** the next start logs that the override was dropped and uses `resident.account`

### Requirement: The daemon switches the resident off a depleted account automatically

When `[resident] auto_switch_account` is on, the resident manager SHALL, on each ensure tick with a live resident session, move the resident to the eligible account with the most headroom when ALL hold: the current account's usage reading has a live window (valid and fresh per the usage cache rules) at 100% or more; the session is idle and the resident is not waiting on a confirmation; no move, recycle, restart or graduation of the resident is in flight; the condition held on two consecutive ticks; and at least 30 minutes have passed since the last automatic switch (persisted across daemon restarts). A missing, stale or unparseable reading SHALL never trigger a switch. Eligible targets are Claude-capable accounts other than the current one whose own live reading is not depleted; an account with no reading is eligible and counts as 0% usage, and the lowest-usage eligible account wins. The switch SHALL use the same move as the manual path without forcing, so no running turn is interrupted. If no account is eligible the manager SHALL not move, SHALL log once per distinct situation and post one board item per episode, and SHALL keep re-evaluating on later ticks. No new usage threshold SHALL be introduced.

#### Scenario: Depleted and idle switches to the account with most headroom

- **WHEN** the resident's account reads 100% on its five-hour window, the session is idle for two ticks, and accounts `a` (40%) and `b` (10%) are eligible
- **THEN** the resident moves to `b`, resuming its conversation

#### Scenario: Never interrupts a turn

- **WHEN** the current account is depleted but the session is mid-turn
- **THEN** no switch happens until the session is idle

#### Scenario: Does not move to another depleted account

- **WHEN** every other account's live reading is at 100%
- **THEN** no move occurs, one log line and one board item report that no account is eligible, and a later tick switches as soon as an account recovers

#### Scenario: Cooldown prevents flapping

- **WHEN** an automatic switch happened 10 minutes ago, including before a daemon restart, and the new account also reads depleted
- **THEN** no further automatic switch occurs until 30 minutes have passed

#### Scenario: Unknown usage does not trigger

- **WHEN** the current account has no reading or only a stale one
- **THEN** no switch occurs

#### Scenario: Setting off

- **WHEN** `auto_switch_account` is false and the current account is depleted
- **THEN** the manager never switches the resident automatically; the manual move still works

### Requirement: Automatic switches are visible to the human and to the resident

Every automatic switch SHALL be logged under the `[resident]` uxlog prefix and through slog with the from and to accounts, the readings and whether the conversation resumed. It SHALL send the resident a reliable system turn beginning `[argus] ` stating the switch and that its conversation carried over, and SHALL post an `info` item to the resident coordinator's human board naming the accounts and the depleted reading. A refusal because no account is eligible SHALL post a `blocked` board item. Manual moves SHALL be logged the same way without a board item.

#### Scenario: Board item on switch

- **WHEN** the resident is automatically switched from `work` to `personal`
- **THEN** the resident board shows an item "Solo switched accounts: work to personal" and the resident receives an `[argus]` system turn saying so

## MODIFIED Requirements

### Requirement: Resident configuration

The system SHALL read a `[resident]` config block with `enabled` (default false), `name` (no default; required to enable; 1 to 12 printable runes, validated), `context_budget` (default the coordinator default), `memory_dir` (default `resident`), `model`, `account`, `auto_switch_account` (default true), `allow_tools`, `ask_tools` and `deny_tools`. `account` is the preferred account; a runtime account override created by a resident move outranks it until `account` is edited. All resident behavior SHALL log under the `[resident]` uxlog prefix, including start, stop, restarts, memory-prerequisite failures, signal changes and account switches.

#### Scenario: Defaults

- **WHEN** no `[resident]` block exists
- **THEN** the resident is disabled, has no name, uses the default budget and `memory_dir=resident`, and `auto_switch_account` is true

#### Scenario: Name required to enable

- **WHEN** `enabled` is true but `name` is empty, longer than 12 runes, or contains control characters
- **THEN** the resident does not start, no tab appears, and a `[resident]` log line states that a valid name is required

#### Scenario: Name is the opt-in

- **WHEN** the user sets a valid name in Settings and enables the resident
- **THEN** the resident starts and the tab appears labelled with that name
