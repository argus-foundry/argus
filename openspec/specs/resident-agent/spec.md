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

The resident's spawn environment SHALL carry `ARGUS_RESIDENT=1` alongside `ARGUS_TASK_ID`, and no other task's SHALL. The coord-hook SHALL ask the daemon whether the task is the resident only when that variable is set and the task has no Hera role, so plain tasks incur no added Stop-time RPC. Hera coordinator, worker and freelance behavior SHALL be unchanged.

#### Scenario: Plain task Stop

- **WHEN** a task without `ARGUS_RESIDENT` and without a Hera role stops
- **THEN** the coord-hook makes no resident lookup and exits silently

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

The resident's permission policy SHALL have three tiers, configurable by `resident.allow_tools`, `resident.ask_tools` and `resident.deny_tools` with shipped defaults, and SHALL run under a non-bypass permission mode so the ask and deny tiers are enforced: read-only connector tools and `kb_*` SHALL run without a prompt; outward tools (every write/send/delete/modify tool of the outward connectors (Notion, Gmail, Superhuman, Slack, Linear, Google Calendar, Granola), except draft creation/update) SHALL require an explicit user confirmation through the Claude permission prompt; Slack message sending (including scheduled sends) SHALL be denied, with draft creation/update (such as `slack_send_message_draft`) allowed; Slack send and schedule are denied. A pending confirmation SHALL raise the resident's waiting (`?`) signal.

The resident launch SHALL strip `--dangerously-skip-permissions`, `--allow-dangerously-skip-permissions` and `--permission-mode` (with its value) from the backend command, and SHALL then apply a flag allowlist to what remains: the executable SHALL be a Claude binary (any path whose base name is `claude`) and every other word SHALL be `--model`, `--effort`, `--fallback-model` (each with a value, space or `=` form) or `--verbose`, free of quotes and shell metacharacters. Any other word (including `--allowedTools`/`--allowed-tools`, `--plugin-dir`, `--plugin-url`, `--agent`, `--agents`, `--mcp-config`, `--settings`, `--setting-sources`, `--add-dir`, positional words and shell operators) SHALL make the launch fail with an error naming it, surfaced as the resident's reason; plain tasks are unaffected.

The policy SHALL be self-contained: the resident SHALL launch with setting sources restricted to `project` so the user's own settings (allow rules, hooks, plugins, model) do not apply, and only the resident's own allow list SHALL run without a prompt. The user's `permissions.deny` and `permissions.ask` rules SHALL be merged into the resident's lists (never the user's `allow` rules), its inline settings SHALL set the default permission mode and disable bypass mode, SHALL carry the account's `model` and `effortLevel` when no other model is chosen and the account's `sandbox` settings block by allowlist (only `enabled`, `failIfUnavailable`, `network.deniedDomains`, `filesystem.denyRead` and `filesystem.denyWrite` are carried; every other key, including unknown ones and `credentials.*`, is dropped; nothing carried when none remains) with its Bash auto-approve and unsandboxed-command escapes forced off, except that `enabled` and `failIfUnavailable` SHALL NOT be carried when the resident itself runs under Argus's sandbox (user hooks are never carried; skipped hook events are logged), and SHALL register the `argus coord-hook` Stop hook inline in place of the dropped user-level hook. The account's `CLAUDE.md` and user `skills/` SHALL still be provided to the resident. Sending a message to another task (`task_message_send`) SHALL require confirmation, while reading and acknowledging the resident's own inbox SHALL NOT.

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

#### Scenario: Resident backend flag allowlist

- **WHEN** the resident's backend command is `claude --model sonnet` or `/opt/x/claude --model=opus`
- **THEN** it launches
- **WHEN** it contains `--allowedTools`, `--plugin-dir`, `--mcp-config`, `--settings`, `--add-dir` or a positional word
- **THEN** the launch fails with an error naming the flag, and a plain task with the same command still launches

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

The system SHALL read a `[resident]` config block with `enabled` (default false), `name` (no default; required to enable; 1 to 12 printable runes, validated), `context_budget` (default the coordinator default), `memory_dir` (default `resident`), `model`, `account`, `allow_tools`, `ask_tools` and `deny_tools`. All resident behavior SHALL log under the `[resident]` uxlog prefix, including start, stop, restarts, memory-prerequisite failures and signal changes.

#### Scenario: Defaults

- **WHEN** no `[resident]` block exists
- **THEN** the resident is disabled, has no name, and uses the default budget and `memory_dir=resident`

#### Scenario: Name required to enable

- **WHEN** `enabled` is true but `name` is empty, longer than 12 runes, or contains control characters
- **THEN** the resident does not start, no tab appears, and a `[resident]` log line states that a valid name is required

#### Scenario: Name is the opt-in

- **WHEN** the user sets a valid name in Settings and enables the resident
- **THEN** the resident starts and the tab appears labelled with that name

