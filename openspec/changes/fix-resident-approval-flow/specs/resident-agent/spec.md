## MODIFIED Requirements

### Requirement: The resident's current account persists and a manual move lasts one session

The account the resident currently runs on SHALL be persisted on its row and in a runtime marker in the resident's task meta (`account_override`: account, `by` = `manual` or `auto`, time) so restarts and daemon bounces do not jump accounts. Every restart of the resident (a cold start, a restart after an exit or crash, and a self-recycle) SHALL re-pick the best account per the list; a reattach to a live process SHALL NEVER change the account (the stored account stays equal to the account the process runs on). A restart that resumes a conversation SHALL reach a different account only through the move core (transcript copy, cooldown stamp, `by=auto` marker, `[argus]` turn, `fyi` board item), so the conversation follows; a recycle has no conversation to carry, so it only sets the account (marker, cooldown stamp and board item, with the notice carried in the new session's seed prompt); a first start with no conversation yet picks per the list. The re-pick keeps a healthy listed account unless a MORE preferred listed account shows a FRESH reading with headroom (an unknown or stale reading never counts as recovered) and the automatic-switch cooldown is not running (so a move's own restart can never bounce the resident again); a depleted, delisted or invalid current account is replaced by the list's first usable account (unknown readings count as usable when leaving). A manual pick is not a hold: it is a one-time move for the current session, and the next restart or recycle re-picks per the list; only the restart the manual move itself causes SHALL NOT re-pick, tracked by a persisted one-shot flag that the next start or recycle consumes (it survives a daemon restart and a long restart backoff, and a manual move resets that backoff so a dead resident launches promptly on the picked account). Any other restart or recycle SHALL also end a manual pick's exemption from the off-list rule, even when the re-pick stays put, so a manual pick of an off-list account cannot pin the resident there. With `auto_switch_account` off a restart re-picks nothing beyond the invalid-account recovery. The one special case is a resuming start whose stored account is no longer valid for the resident (removed from config, or without a Claude login directory), which BuildCmd would refuse forever: it SHALL move the resident onto the list's first usable account, record an automatic marker and the cooldown, tell the resident with an `[argus]` turn and post an `fyi` board item. When the old account's storage can still be read the conversation is copied and resumed; when it cannot (the usual case for a removed account) the resident starts fresh on the new account and is told its conversation could not be carried over. If no listed account can run the resident it SHALL post one `fyi` board item per episode (refreshing an open item with the same subject instead of adding another) and not spin. A manual move SHALL be able to pick any Claude-capable account. A manual pick (named or `auto`) SHALL be a one-time move: while the session keeps running it is treated like any current account (the tick moves off it only when it is depleted, and the off-list rule leaves a manually chosen account alone), but it does NOT survive a restart or recycle, which re-pick per the list. Only an automatic move starts the cooldown. Neither a move nor the marker SHALL modify `resident.accounts`.

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

#### Scenario: A manual pick of an off-list account does not pin the resident

- **WHEN** the human picked an account not on the list, a later restart stayed on it because every listed account was depleted, and a listed account then recovers
- **THEN** the running session moves off the off-list account

#### Scenario: Manual move on a backed-off resident

- **WHEN** the resident is down in a long restart backoff and the human picks `work`
- **THEN** it launches promptly on `work` and the restart-time re-pick does not undo the pick

#### Scenario: Recycle re-picks without a transcript

- **WHEN** the resident is recycled while a more preferred account has a fresh reading with headroom and the cooldown has passed
- **THEN** the fresh session runs on that account, no transcript is copied, and the seed prompt and a board item carry the notice

### Requirement: The daemon moves the resident along its account list automatically

When `[resident] auto_switch_account` is on, the resident manager SHALL, on each ensure tick with a live resident session, compute the account the list wants (the first allowed, non-depleted entry, see the preference-list requirement) and move the resident to it when EITHER the current account is depleted, OR the current account is no longer allowed by the list and was not chosen manually. A running session SHALL NEVER be moved back up to a more preferred account that recovered: it stays where it is while it has room, and moving back up happens only at the next restart or recycle (see the persistence requirement), so Solo is not bounced between accounts. The move SHALL happen only when ALL hold: the session is parked, meaning raw-idle or content-idle (its emulated screen is stable with no working affordance, so a limit-wait screen that keeps repainting, such as Claude Code's "Continuing automatically at ... Esc to cancel" footer, counts) (a needs-input state does NOT block a move OFF A DEPLETED ACCOUNT, because Claude Code's usage-limit menu is itself a prompt and blocking on it would deadlock exactly the case this exists for; the off-list move additionally requires no pending prompt and no unsent composer draft); no move, recycle, restart or graduation of the resident is in flight; the condition held on two consecutive ticks; and at least 10 minutes have passed since the last automatic move (persisted across daemon restarts; depletion moves, off-list moves and restart-time moves share the cooldown). A missing, stale or unparseable reading SHALL never count as depleted. The move SHALL use the same move as the manual path, forced only after the parked check passed (so the move's raw-idle busy refusal cannot block a limit-parked session); a working screen never counts as parked, so a running turn is never interrupted. A weekly window at 100% SHALL count as depleted even when the five-hour window is far below. When the current account is depleted and no listed account is usable the manager SHALL not move, SHALL log once per distinct situation and post one `fyi` board item per episode, refreshing an open item with the same subject instead of adding another (the episode flag is persisted, so a daemon restart does not repost it), and SHALL keep re-evaluating on later ticks. With `auto_switch_account` off the list only decides the account at start. No new usage threshold SHALL be introduced.

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

Every automatic switch SHALL be logged under the `[resident]` uxlog prefix and through slog with the from and to accounts, the readings and whether the conversation resumed. It SHALL send the resident a reliable system turn beginning `[argus] ` stating the switch and telling it to continue whatever it was doing (for example "You were switched from X to Y because X hit its usage limit. Your conversation carried over. Continue whatever you were doing."), and SHALL post an `fyi` item to the resident coordinator's human board naming the accounts and the depleted reading. A refusal because no account is eligible SHALL post an `fyi` board item, because there is nothing for the human to decide. Every resident account notice SHALL refresh an open board item with the same kind and subject (new body, new timestamp, marked unread) instead of posting a duplicate; a new item is posted only when no open one exists. Manual moves SHALL be logged the same way without a board item.

#### Scenario: Board item on switch

- **WHEN** the resident is automatically switched from `work` to `personal`
- **THEN** the resident board shows an item "Solo switched accounts: work to personal" and the resident receives an `[argus]` system turn saying so

#### Scenario: Repeated notice refreshes one item

- **WHEN** the resident is moved from `default` to `personal` twice while the first "moved accounts" item is still open
- **THEN** the board keeps one open item with that subject, showing the latest body and marked unread

#### Scenario: Nothing to decide

- **WHEN** every listed account is depleted
- **THEN** the board item is an `fyi`, not a `decision`
