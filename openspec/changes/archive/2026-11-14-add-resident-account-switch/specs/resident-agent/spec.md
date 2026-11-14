## ADDED Requirements

### Requirement: The resident's accounts are an ordered preference list

The system SHALL replace the single `resident.account` setting with `resident.accounts`: an ordered list of Claude account names (`default` included) that MAY end with the entry `auto`. The resident SHALL run on the first listed account that exists, supports Claude and is not depleted. `auto` means any Claude-capable account, chosen by most headroom (lowest current usage, depleted accounts filtered out, an account with no reading counting as 0%); it is allowed alone or as the last entry ("then anything else"), and any entry after the first `auto` is ignored with a log line. Entries naming an unknown account, an account without a Claude login, or a duplicate are skipped with a log line. An empty or unset list SHALL mean `[default]`, so existing setups keep running on the default account. Depleted means a live (valid, fresh) five-hour or weekly window at 100% or more; a missing or stale reading is never depleted. If every listed account is depleted the resident SHALL stay where it is (a first start falls back to the first usable listed account). config.toml form: `[resident] accounts = ["personal", "default"]` (or ending in `"auto"`); a config.toml value overrides the Settings value. A pre-existing single `resident.account` database value, or a single `account` key in config.toml, SHALL be read as a one-element list (config.toml logs a deprecation and shows the setting read-only in Settings) so an account chosen under the old setting is preserved; the single-account setting is otherwise removed from Settings and the API references. The usage poller SHALL probe every listed account (every Claude-capable account when the list ends in `auto`) while the resident is enabled so a depleted account the resident left can be seen recovering.

#### Scenario: First non-depleted listed account wins

- **WHEN** `accounts = ["personal", "work"]` and `personal` is depleted
- **THEN** a fresh resident start runs on `work`

#### Scenario: Auto as last entry

- **WHEN** `accounts = ["personal", "auto"]`, `personal` is depleted, and `a` (40%) and `b` (10%) are usable
- **THEN** the resident runs on `b`

#### Scenario: Default list

- **WHEN** no accounts are configured
- **THEN** the list is `[default]`

#### Scenario: Old single account is migrated

- **WHEN** the database holds `resident.account = personal` and no `resident.accounts`
- **THEN** the effective list is `["personal"]`

#### Scenario: Invalid entries are skipped

- **WHEN** the list names a deleted account and a Codex-only account before a valid one
- **THEN** both are skipped with a log line and the valid one is used

### Requirement: The resident's current account persists and a manual move lasts one session

The account the resident currently runs on SHALL be persisted on its row and in a runtime marker in the resident's task meta (`account_override`: account, `by` = `manual` or `auto`, time) so restarts and daemon bounces do not jump accounts. Every restart of the resident (a cold start, a restart after an exit or crash, and a self-recycle) SHALL re-pick the best account per the list; a reattach to a live process SHALL NEVER change the account (the stored account stays equal to the account the process runs on). A restart that resumes a conversation SHALL reach a different account only through the move core (transcript copy, cooldown stamp, `by=auto` marker, `[argus]` turn, `fyi` board item), so the conversation follows; a recycle has no conversation to carry, so it only sets the account (marker, cooldown stamp and board item, with the notice carried in the new session's seed prompt); a first start with no conversation yet picks per the list. The re-pick keeps a healthy listed account unless a MORE preferred listed account shows a FRESH reading with headroom (an unknown or stale reading never counts as recovered) and the automatic-switch cooldown is not running (so a move's own restart can never bounce the resident again); a depleted, delisted or invalid current account is replaced by the list's first usable account (unknown readings count as usable when leaving). A manual pick is not a hold: it is a one-time move for the current session, and the next restart or recycle re-picks per the list; only the restart the manual move itself performs (within two minutes of it) SHALL NOT re-pick. With `auto_switch_account` off a restart re-picks nothing beyond the invalid-account recovery. The one special case is a resuming start whose stored account is no longer valid for the resident (removed from config, or without a Claude login directory), which BuildCmd would refuse forever: it SHALL move the resident onto the list's first usable account, record an automatic marker and the cooldown, tell the resident with an `[argus]` turn and post an `fyi` board item. When the old account's storage can still be read the conversation is copied and resumed; when it cannot (the usual case for a removed account) the resident starts fresh on the new account and is told its conversation could not be carried over. If no listed account can run the resident it SHALL post one `decision` board item per episode and not spin. A manual move SHALL be able to pick any Claude-capable account. A manual pick (named or `auto`) SHALL be a one-time move: while the session keeps running it is treated like any current account (the tick moves off it only when it is depleted, and the off-list rule leaves a manually chosen account alone), but it does NOT survive a restart or recycle, which re-pick per the list. Only an automatic move starts the cooldown. Neither a move nor the marker SHALL modify `resident.accounts`.

#### Scenario: Move survives a crash restart

- **WHEN** the resident was moved to `personal` and its session later crashes
- **THEN** the ensure loop restarts it on `personal` and resumes the conversation

#### Scenario: Manual move outside the list lasts for the session

- **WHEN** the list is `["work"]` and the human moves the resident to `personal`, which is not depleted
- **THEN** the running session stays on `personal` until it is depleted, and the next restart re-picks per the list

#### Scenario: Removed account recovers on restart

- **WHEN** the resident's stored account was removed from config and its session restarts
- **THEN** it starts on the list's next valid account, the resident is told whether its conversation carried over, and a board item records the move

#### Scenario: Restart moves off a depleted or delisted account, keeping the conversation

- **WHEN** the resident's session crashes while its account is depleted or no longer listed
- **THEN** the restart moves it through the move core to the list's first usable account, resumes the same conversation there, and sends one notice and one board item

#### Scenario: Restart picks the preferred account with a fresh reading

- **WHEN** the resident is on `work`, `personal` (more preferred) has a fresh reading with headroom, the cooldown has passed, and the session restarts
- **THEN** it moves to `personal` through the move core and resumes the conversation there

#### Scenario: Restart stays when the preferred reading is stale

- **WHEN** the resident is on `work` and `personal`'s reading is stale or unknown at a restart
- **THEN** it stays on `work`

#### Scenario: A move's own restart does not move again

- **WHEN** an automatic move off a depleted account has just happened and the preferred account recovers before the move's restart completes
- **THEN** the restart stays on the new account (one move, one notice)

#### Scenario: A manual pick does not survive a restart

- **WHEN** the human picked `work` while a more preferred account has room
- **THEN** the move's own restart stays on `work`, and a later restart or recycle re-picks the preferred account (fresh reading, cooldown clear)

#### Scenario: Recycle re-picks without a transcript

- **WHEN** the resident is recycled while a more preferred account has a fresh reading with headroom and the cooldown has passed
- **THEN** the fresh session runs on that account, no transcript is copied, and the seed prompt and a board item carry the notice

### Requirement: The resident can move between Claude accounts and keeps its conversation

The system SHALL let the resident move to another Claude account through a daemon-side path only (RPC `Daemon.ResidentMoveAccount`), accepting a configured account name, `default`, or `auto` (the eligible other account with the lowest current usage, depleted accounts filtered out). The move SHALL copy the resident's saved conversation into the target account's storage, persist the new account, and restart a live session in place with resume, with no model turn. The persist step SHALL write the resident row's account and session id and the account marker in one transaction before the restart. A busy session SHALL be refused unless the caller forces it. The generic task move paths (`task_move_account`, REST and the task `>` picker) and every backend (Claude to Codex) move SHALL continue to refuse the resident. A resident with no saved conversation SHALL still switch accounts and start fresh.

#### Scenario: Manual move carries the conversation

- **WHEN** the resident is idle on account `work` with a saved conversation and the human moves it to `personal`
- **THEN** the transcript is copied to `personal`, the row and marker record `personal`, and the session restarts in place resuming the same conversation

#### Scenario: Generic move still refuses the resident

- **WHEN** `task_move_account` or the REST move names the resident's task id
- **THEN** it is refused with the resident-protected error and nothing is copied

#### Scenario: Busy session refused

- **WHEN** the resident is mid-turn and the move is not forced
- **THEN** the move is refused as busy and nothing changes

### Requirement: The daemon moves the resident along its account list automatically

When `[resident] auto_switch_account` is on, the resident manager SHALL, on each ensure tick with a live resident session, compute the account the list wants (the first allowed, non-depleted entry, see the preference-list requirement) and move the resident to it when EITHER the current account is depleted, OR the current account is no longer allowed by the list and was not chosen manually. A running session SHALL NEVER be moved back up to a more preferred account that recovered: it stays where it is while it has room, and moving back up happens only at the next restart or recycle (see the persistence requirement), so Solo is not bounced between accounts. The move SHALL happen only when ALL hold: the session is parked, meaning raw-idle or content-idle (its emulated screen is stable with no working affordance, so a limit-wait screen that keeps repainting, such as Claude Code's "Continuing automatically at ... Esc to cancel" footer, counts) (a needs-input state does NOT block a move OFF A DEPLETED ACCOUNT, because Claude Code's usage-limit menu is itself a prompt and blocking on it would deadlock exactly the case this exists for; the off-list move additionally requires no pending prompt and no unsent composer draft); no move, recycle, restart or graduation of the resident is in flight; the condition held on two consecutive ticks; and at least 10 minutes have passed since the last automatic move (persisted across daemon restarts; depletion moves, off-list moves and restart-time moves share the cooldown). A missing, stale or unparseable reading SHALL never count as depleted. The move SHALL use the same move as the manual path, forced only after the parked check passed (so the move's raw-idle busy refusal cannot block a limit-parked session); a working screen never counts as parked, so a running turn is never interrupted. A weekly window at 100% SHALL count as depleted even when the five-hour window is far below. When the current account is depleted and no listed account is usable the manager SHALL not move, SHALL log once per distinct situation and post one `decision` board item per episode (the episode flag is persisted, so a daemon restart does not repost it), and SHALL keep re-evaluating on later ticks. With `auto_switch_account` off the list only decides the account at start. No new usage threshold SHALL be introduced.

#### Scenario: Depleted moves to the next listed account

- **WHEN** `accounts = ["personal", "work", "auto"]`, the resident is on `personal` at 100% weekly, and `work` has room
- **THEN** once the session is parked for two ticks the resident moves to `work`, resuming its conversation, even if another account has lower usage

#### Scenario: Weekly limit wait screen switches

- **WHEN** the current account's weekly window is 100% with its five-hour window at 0%, and the session shows the limit-wait footer while repainting (never raw-idle)
- **THEN** once the screen has been stable for the idle threshold the resident moves to the next usable account

#### Scenario: A running session never moves back up

- **WHEN** the resident is on `work` after `personal` was depleted, `personal`'s window then resets, and the session keeps running with room on `work`
- **THEN** it stays on `work` until its next restart or recycle

#### Scenario: Off-list move waits for a quiet session

- **WHEN** the current account was removed from the list but the resident has a pending prompt or an unsent draft
- **THEN** it waits, while a move off a depleted account would not

#### Scenario: Manual choice is not moved

- **WHEN** the human moved the resident to `work` while `personal` (more preferred) had room
- **THEN** no move occurs; only depletion of `work` triggers the next automatic move

#### Scenario: Never interrupts a turn

- **WHEN** a move is due but the session is mid-turn
- **THEN** no move happens until the session is parked

#### Scenario: Usage-limit prompt does not block

- **WHEN** the current account is depleted, the session is parked, and the resident is flagged as needing input (for example Claude Code's usage-limit menu)
- **THEN** the move proceeds

#### Scenario: Every listed account depleted

- **WHEN** every listed account's live reading is at 100%
- **THEN** no move occurs, one log line and one board item report that no account is usable, and a later tick moves as soon as one recovers

#### Scenario: Cooldown prevents flapping

- **WHEN** an automatic move happened 5 minutes ago, including before a daemon restart
- **THEN** no further automatic move occurs until 10 minutes have passed

#### Scenario: Unknown usage does not trigger

- **WHEN** the current account has no reading or only a stale one
- **THEN** no depletion move occurs

#### Scenario: Setting off

- **WHEN** `auto_switch_account` is false and the current account is depleted
- **THEN** the manager never moves the resident automatically; the manual move still works

### Requirement: Automatic switches are visible to the human and to the resident

Every automatic switch SHALL be logged under the `[resident]` uxlog prefix and through slog with the from and to accounts, the readings and whether the conversation resumed. It SHALL send the resident a reliable system turn beginning `[argus] ` stating the switch and telling it to continue whatever it was doing (for example "You were switched from X to Y because X hit its usage limit. Your conversation carried over. Continue whatever you were doing."), and SHALL post an `fyi` item to the resident coordinator's human board naming the accounts and the depleted reading. A refusal because no account is eligible SHALL post a `decision` board item. Manual moves SHALL be logged the same way without a board item.

#### Scenario: Board item on switch

- **WHEN** the resident is automatically switched from `work` to `personal`
- **THEN** the resident board shows an item "Solo switched accounts: work to personal" and the resident receives an `[argus]` system turn saying so

## MODIFIED Requirements

### Requirement: Resident configuration

The system SHALL read a `[resident]` config block with `enabled` (default false), `name` (no default; required to enable; 1 to 12 printable runes, validated), `context_budget` (default the coordinator default), `memory_dir` (default `resident`), `model`, `accounts` (ordered preference list, default `["default"]`, see the preference-list requirement), `auto_switch_account` (default true), `allow_tools`, `ask_tools` and `deny_tools`. All resident behavior SHALL log under the `[resident]` uxlog prefix, including start, stop, restarts, memory-prerequisite failures, signal changes and account switches.

#### Scenario: Defaults

- **WHEN** no `[resident]` block exists
- **THEN** the resident is disabled, has no name, uses the default budget and `memory_dir=resident`, and `auto_switch_account` is true

#### Scenario: Name required to enable

- **WHEN** `enabled` is true but `name` is empty, longer than 12 runes, or contains control characters
- **THEN** the resident does not start, no tab appears, and a `[resident]` log line states that a valid name is required

#### Scenario: Name is the opt-in

- **WHEN** the user sets a valid name in Settings and enables the resident
- **THEN** the resident starts and the tab appears labelled with that name
