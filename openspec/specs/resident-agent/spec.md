# resident-agent Specification

## Purpose
TBD - created by archiving change add-resident-agent. Update Purpose after archive.
## Requirements
### Requirement: The resident is a singleton task row with no git worktree

The system SHALL represent the resident as exactly one task row whose `kind` is `resident`, created lazily by the daemon the first time the resident is enabled. A partial unique index SHALL prevent a second resident row. The row SHALL have `Name="resident"`, empty `Project` and `Branch`, `Status=in_progress`, and a non-empty `Worktree` equal to the resident working directory `~/.argus/resident/`, a plain directory (not a git worktree) that the daemon creates. `BuildCmd` SHALL accept a task with no git worktree only when its `Worktree` is exactly the resident working directory and its kind is `resident`. No path other than the daemon's resident-enable flow SHALL be able to create a resident row (not `task_create`, the TUI, REST, or `schedule_*`).

#### Scenario: First enable creates the row

- **WHEN** `resident.enabled` becomes true and no resident row exists
- **THEN** the daemon creates one `kind=resident` row with the resident working directory and starts its session

#### Scenario: A second resident is impossible

- **WHEN** any code path attempts to insert a second `kind=resident` row
- **THEN** the insert fails and the existing row is unchanged

#### Scenario: Other worktree-less tasks still refuse to start

- **WHEN** a non-resident task with an empty `Worktree`, or a resident-kind task whose `Worktree` is not the resident working directory, is started
- **THEN** `BuildCmd` refuses to start it

### Requirement: The resident is never lifecycle-managed and never listed as a task

The system SHALL expose one predicate, `IsResident()`, and SHALL apply it so that the resident is excluded from every user-facing task enumeration and every status-driven or reaping mechanism. Excluded surfaces SHALL include: the Tasks list and rail, the jump switcher, the Hera roster, status counts, REST `/api/tasks*`, MCP `task_list` and `task_get`, task SSE events, the finished-session reaper, the devstack sweeper, the orphan sweep, auto-prune, the PR poller, merge-safety cleanup and reconciliation roll-ups. Lifecycle verbs (complete, archive, delete, set status, fork, account move) SHALL reject the resident with an explicit error. Resident events SHALL be published under a distinct `resident.*` event type.

#### Scenario: Absent from task views

- **WHEN** a resident row exists and the Tasks list, jump switcher, REST task list and MCP `task_list` are queried
- **THEN** none of them includes it and status counts do not count it

#### Scenario: Sweepers and reapers leave it alone

- **WHEN** the finished-session reaper, devstack sweeper, orphan sweep and auto-prune tick with an idle resident session
- **THEN** none stops, removes or marks the resident

#### Scenario: Lifecycle verbs reject it

- **WHEN** complete, archive, delete, fork, set-status or account-move is requested for the resident
- **THEN** the request fails with an explicit error naming the resident and nothing changes

#### Scenario: A new enumeration cannot silently include it

- **WHEN** the enumerating test `TestResidentExcludedEverywhere` runs against every listed choke point with a resident row present
- **THEN** every choke point excludes the resident

### Requirement: The daemon keeps the resident session running

While `resident.enabled` is true the daemon SHALL ensure the resident session is running: after supervisor re-attach at daemon start, when the setting becomes true, and, with bounded backoff, when the session exits unexpectedly. It SHALL resume the existing Claude session; an intentional recycle relaunches it itself with the recycle seed. A recycle seed is one-shot: every launch the daemon makes that is not the recycle itself (first start, re-enable, or a fresh launch because no resumable transcript exists) SHALL use the resident's orientation prompt, never a persisted recycle seed. The session SHALL be owned by the supervisor like any agent PTY, so it survives TUI exit and daemon restarts. When `resident.enabled` becomes false the daemon SHALL stop the session and SHALL leave the row and the vault memory folder intact. When a prerequisite (the resident requirements below, or the Claude-family backend) fails on two consecutive ticks while the session is running, the daemon SHALL stop the session the same way and surface the failure reason, and SHALL start it again once the prerequisite recovers; a single failing tick SHALL NOT stop it, and the daemon's own MCP server not having bound yet is a startup wait, not a prerequisite failure. The resident SHALL use the default Claude-family backend, with optional `resident.model` and `resident.account` overrides, and SHALL run under the normal sandbox resolution. The daemon SHALL launch the resident at the resident tab's own reported pane size (the TUI's published resident entry, not the size of whatever view the user is on), falling back to the live session's PTY size (a re-attach, or a TUI that publishes no resident size) and then to 24x80.

#### Scenario: Survives TUI exit and daemon bounce

- **WHEN** the TUI exits or the daemon restarts while the resident is running
- **THEN** the session keeps running and the daemon re-attaches to it

#### Scenario: Crash recovery with backoff

- **WHEN** the resident session exits unexpectedly while enabled
- **THEN** the daemon restarts it with bounded backoff and resumes the prior session, logging each attempt

#### Scenario: Recycle seed is one-shot

- **WHEN** the resident was recycled and later launches fresh (for example after `resident.account` changes and the prior transcript cannot be resumed)
- **THEN** the launch carries the orientation prompt, not the earlier recycle's handoff note or relay directive

#### Scenario: Disable stops but preserves

- **WHEN** `resident.enabled` is set to false
- **THEN** the session is stopped and the row and `resident/` vault folder are unchanged

#### Scenario: Prerequisite fails while running

- **WHEN** the resident is running and a prerequisite later fails (for example `api.enabled` is switched off or the vault path disappears) on two consecutive ticks
- **THEN** the session is stopped, the failure reason is shown in the resident state, and the daemon restarts it once the prerequisite recovers

#### Scenario: Transient prerequisite blip

- **WHEN** a prerequisite fails for a single tick and then recovers
- **THEN** the running session is not stopped

#### Scenario: Non-Claude default backend

- **WHEN** the default backend is not Claude-family
- **THEN** the resident uses the default Claude-family backend or refuses to start with an explicit reason

### Requirement: The resident requires the knowledge base

The system SHALL start the resident only when `kb.enabled` is true and `metis_vault_path` resolves to an existing directory. Otherwise it SHALL NOT start, the Settings Resident category SHALL show the missing prerequisite, and the daemon SHALL log it under the `[resident]` prefix.

#### Scenario: KB disabled

- **WHEN** the user enables the resident while the KB is disabled
- **THEN** the session does not start and Settings names the missing prerequisite

### Requirement: The resident requires the REST API

The system SHALL start the resident only when `api.enabled` is true, because the resident's reply stamping, context budget reads, notification delivery and waiting signal are carried by the REST API and its idle watcher. Otherwise it SHALL NOT start and the prerequisite error SHALL name `api.enabled` and how to set it, shown in Settings and logged under the `[resident]` prefix.

#### Scenario: API disabled

- **WHEN** the resident is enabled while `api.enabled` is false
- **THEN** the session does not start and Settings shows an error telling the user to enable `api.enabled`

### Requirement: The Stop hook consults the daemon only for the resident

The resident's spawn environment SHALL carry `ARGUS_RESIDENT=1` alongside `ARGUS_TASK_ID`, and no other task's SHALL. The coord-hook SHALL ask the daemon whether the task is the resident only when that variable is set, so plain tasks incur no added Stop-time RPC. Because the resident now holds a Hera coordinator role, the hook SHALL NOT use the presence of a Hera role to skip or to select resident behavior: for the resident the resident path (reply capture, `context_size` stamp, budget nudge) runs, and the generic coordinator budget, nudge and recycle-request behavior SHALL NOT apply to it. Hera coordinator, worker and freelance behavior for every other task SHALL be unchanged.

#### Scenario: Plain task Stop

- **WHEN** a task without `ARGUS_RESIDENT` and without a Hera role stops
- **THEN** the coord-hook makes no resident lookup and exits silently

#### Scenario: Resident Stop with a Hera role

- **WHEN** the resident, which holds a coordinator role, stops
- **THEN** the hook records the reply and context size through the resident path and does not also apply the coordinator budget nudge

#### Scenario: Other coordinators unchanged

- **WHEN** a regular coordinator stops
- **THEN** the coordinator nudge and stamp behave exactly as before

### Requirement: Working memory lives in a fenced folder of the Metis vault

The resident SHALL keep working memory in the vault-relative folder `resident/` (configurable as `resident.memory_dir`, independent of the display name): `open-loops.md`, `profile.md`, `preferences.md` and `log/YYYY-MM-DD.md`. Every working file SHALL carry the KB-required `title` and `tags` frontmatter plus `dream: skip`. All vault I/O SHALL go through the `kb_*` MCP tools. The resident's orientation prompt and every recycle seed SHALL instruct it to read `resident/open-loops.md` before any other action. Renaming the agent SHALL NOT move or rewrite the folder.

#### Scenario: Open loops read first

- **WHEN** a fresh resident session starts (first start or after recycle)
- **THEN** its opening prompt instructs it to read `resident/open-loops.md` first

#### Scenario: Working files are fenced

- **WHEN** the resident writes a working-memory file
- **THEN** the file lives under `resident/` and carries `title`, `tags` and `dream: skip` frontmatter

#### Scenario: Rename does not move memory

- **WHEN** the user changes the agent name in Settings
- **THEN** the memory folder path and contents are unchanged

### Requirement: Durable facts go into normal KB notes

The resident SHALL record durable facts (people, customers, projects, decisions) in the ordinary topic notes following the KB schema: `kb_search` first, update an existing note rather than creating a duplicate, kebab-case vault-relative paths, `title` and `tags` frontmatter, wikilink cross-references. Working-memory content SHALL NOT be written into topic notes.

#### Scenario: Search before write

- **WHEN** the resident is told to remember a fact
- **THEN** it searches the KB first and updates an existing note when one covers the topic

### Requirement: Outward actions need one-key confirmation and Slack is drafts only

The resident's permission policy SHALL have three tiers, configurable by `resident.allow_tools`, `resident.ask_tools` and `resident.deny_tools` with shipped defaults, and SHALL run under a non-bypass permission mode so the ask and deny tiers are enforced: all shipped local/built-in tools, non-Hera Argus MCP tools other than `task_message_send`, reviewed connector reads and draft creation/update SHALL run without a prompt; the resident-coordinator specification’s structural Hera allow/ask/deny tiers SHALL remain unchanged; outward tools (every write/send/delete/modify tool of the outward connectors (Notion, Gmail, Superhuman, Slack, Linear, Google Calendar, Granola, Keystone, Figma, Sketch, Claude Docs and artifacts), except draft creation/update) SHALL require an explicit user confirmation through the Claude permission prompt; Slack message sending (including scheduled sends) SHALL be denied, with draft creation/update (such as `slack_send_message_draft`) allowed; Slack send and schedule are denied. Rules SHALL follow deny > ask > allow precedence. Connector allow rules SHALL name a literal server and reviewed read or draft tool, never a whole external server or global MCP allow wildcard. An unmatched tool SHALL require confirmation in default mode. Because Claude Code does not support global MCP allow patterns or expose connector schemas to Argus at launch, reads from wholly unlisted servers MAY still prompt. A pending confirmation SHALL raise the resident's waiting (`?`) signal.

The resident launch SHALL strip `--dangerously-skip-permissions`, `--allow-dangerously-skip-permissions` and `--permission-mode` (with its value) from the backend command, and SHALL then apply a flag allowlist to what remains: the executable SHALL be a Claude binary (any path whose base name is `claude`) and every other word SHALL be `--model`, `--effort`, `--fallback-model` (each with a value, space or `=` form) or `--verbose`, free of shell metacharacters. The executable word SHALL be a plain, unquoted path (no quotes, globs, escapes or `=`, and not starting with `-`, `+` or `%`) and MAY lead with `~/`; a value MAY be a simple single-quoted string (`--model 'opus[1m]'`, `--effort='high'`). Unescaped newlines or carriage returns (a newline ends the command in sh), double quotes, `$`, backticks, `;`, `|`, `&`, redirects, spaces, unbalanced quotes and unquoted `[`/`]` (a shell glob; the error suggests single-quoting) SHALL be refused. Any other word (including `--allowedTools`/`--allowed-tools`, `--plugin-dir`, `--plugin-url`, `--agent`, `--agents`, `--mcp-config`, `--settings`, `--setting-sources`, `--add-dir`, positional words and shell operators) SHALL make the launch fail with an error naming it, surfaced as the resident's reason; plain tasks are unaffected.

The policy SHALL be self-contained: the resident SHALL launch with setting sources restricted to `project` so the user's own settings (allow rules, hooks, plugins, model) do not apply, and only the resident's own allow list SHALL run without a prompt. The user's `permissions.deny` and `permissions.ask` rules SHALL be merged into the resident's lists (never the user's `allow` rules), its inline settings SHALL set the default permission mode and disable bypass mode, SHALL carry the account's `model` and `effortLevel` when no other model is chosen and the account's `sandbox` settings block by allowlist (only `enabled`, `failIfUnavailable`, `network.deniedDomains`, `filesystem.denyRead` and `filesystem.denyWrite` are carried; every other key, including unknown ones and `credentials.*`, is dropped; nothing carried when none remains) with its Bash auto-approve and unsandboxed-command escapes forced off, except that `enabled` and `failIfUnavailable` SHALL NOT be carried when the resident itself runs under Argus's sandbox (user hooks are never carried; skipped hook events are logged), and SHALL register the `argus coord-hook` Stop hook inline in place of the dropped user-level hook. The account's `CLAUDE.md` and user `skills/` SHALL still be provided to the resident. When Argus's sandbox applies to the resident but its profile cannot be generated, the launch SHALL fail with an error rather than start without either sandbox; plain tasks keep falling through unsandboxed. Sending a message to another task (`task_message_send`) SHALL require confirmation, while reading and acknowledging the resident's own inbox SHALL NOT.

#### Scenario: Calendar invite asks first

- **WHEN** the resident attempts to create a calendar event
- **THEN** the call is held for confirmation and the resident's waiting signal is raised

#### Scenario: Slack send is denied, draft allowed

- **WHEN** the resident attempts `slack_send_message`, then `slack_send_message_draft`
- **THEN** the first is denied and the second proceeds without a prompt

#### Scenario: User allow rules do not widen the resident

- **WHEN** the user's settings allow every tool of a connector the resident does not list
- **THEN** the resident's launch carries none of those allow rules and the tool prompts

#### Scenario: Reads do not prompt

- **WHEN** the resident lists calendar events or searches the KB
- **THEN** no confirmation is requested

#### Scenario: Sandbox block is carried by allowlist

- **WHEN** the user's sandbox block holds `enabled`, `network.deniedDomains`, `filesystem.denyRead`, `network.allowedDomains`, `excludedCommands`, `allowAppleEvents` and an unknown key
- **THEN** the resident's inline settings carry only `enabled`, the deny lists and the two forced-false Bash keys

#### Scenario: Sandbox block with nothing restrictive is not carried

- **WHEN** the user's sandbox block holds only loosening or unknown keys
- **THEN** the resident's inline settings contain no `sandbox` block

#### Scenario: Argus-sandboxed resident drops enabled

- **WHEN** the resident runs under Argus's own sandbox and the user's sandbox block has `enabled: true`
- **THEN** `enabled` and `failIfUnavailable` are not carried while the deny lists still are

#### Scenario: Quoted or non-plain executable refused

- **WHEN** the resident's backend command's executable is quoted or not a plain path (e.g. `'/opt/x/claude'` or `A=/opt/claude`)
- **THEN** the resident does not start and the reason names that executable

#### Scenario: Sandbox profile failure fails the resident closed

- **WHEN** Argus's sandbox applies to the resident launch but generating its profile fails
- **THEN** the launch fails with a "resident sandbox profile" error and the resident does not start
- **WHEN** a plain task hits the same failure
- **THEN** it still launches unsandboxed

#### Scenario: Resident backend flag allowlist

- **WHEN** the resident's backend command is `claude --model sonnet` or `/opt/x/claude --model=opus`
- **THEN** it launches
- **WHEN** it is `~/.local/bin/claude --model 'opus[1m]'`
- **THEN** it launches
- **WHEN** it contains a double-quoted or `$HOME` word, or unquoted `opus[1m]`
- **THEN** the launch fails naming the word (the glob error suggests single-quoting)
- **WHEN** it contains `--allowedTools`, `--plugin-dir`, `--mcp-config`, `--settings`, `--add-dir` or a positional word
- **THEN** the launch fails with an error naming the flag, and a plain task with the same command still launches

#### Scenario: Bash runs without a prompt

- **WHEN** the resident invokes Bash for ordinary work with no matching user ask/deny rule
- **THEN** the policy allows it without confirmation and Argus sandbox restrictions still apply

#### Scenario: Previously unlisted connector read runs without a prompt

- **WHEN** the resident invokes Keystone `approved_mcp_servers`, `drata_policies`, `list_directory`, `list_repositories` or `search_code`, absent from the old defaults
- **THEN** the shipped policy allows the read without confirmation

#### Scenario: Mail send asks

- **WHEN** the resident sends, replies to or forwards mail
- **THEN** confirmation is required and the waiting signal is raised

#### Scenario: Calendar event create asks

- **WHEN** the resident calls `create_event`
- **THEN** confirmation is required even when a broader allow also matches

#### Scenario: Slack draft is allowed

- **WHEN** the resident calls `slack_send_message_draft`
- **THEN** it runs without confirmation

#### Scenario: Slack send is denied

- **WHEN** the resident sends or schedules a Slack message
- **THEN** it is denied

#### Scenario: Unknown write tool asks

- **WHEN** the resident calls a new connector tool with no matching read/draft allow rule
- **THEN** it asks rather than running unprompted, including on an otherwise covered server

#### Scenario: User restrictions override broad allow

- **WHEN** the user asks for a Bash command or denies a connector read which the shipped policy allows
- **THEN** ask/deny remains effective, with deny taking precedence

### Requirement: Resident reply and unread state is daemon-held

On every resident Stop the coord-hook SHALL record `task_meta(resident, last_reply)` (first line of `last_assistant_message`, control characters stripped, truncated to 200 runes) and `last_reply_at`. An RPC SHALL set `last_viewed_at` when a frontend shows and focuses the resident pane. **Unread** SHALL be `last_reply_at > last_viewed_at`. **Waiting** SHALL be the sticky needs-input signal computed for the resident task by the existing detector, exposed as its own field because the resident is excluded from the generic task needs-input sets.

#### Scenario: Reply becomes unread

- **WHEN** the resident finishes a turn while its tab is not focused
- **THEN** `last_reply`/`last_reply_at` are updated and the state reads as unread

#### Scenario: Viewing clears unread

- **WHEN** a frontend shows and focuses the resident pane
- **THEN** `last_viewed_at` is set and the state no longer reads as unread

#### Scenario: Preview is bounded

- **WHEN** a reply's first line contains control characters or exceeds 200 runes
- **THEN** the stored preview has the control characters removed and is truncated to 200 runes

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

