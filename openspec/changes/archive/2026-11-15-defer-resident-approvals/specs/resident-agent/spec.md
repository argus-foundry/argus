## MODIFIED Requirements

### Requirement: Outward actions need one-key confirmation and Slack is drafts only

The resident's permission policy SHALL have three tiers, configurable by `resident.allow_tools`, `resident.ask_tools` and `resident.deny_tools` with shipped defaults, and SHALL run under a non-bypass permission mode so the ask and deny tiers are enforced: shipped local/built-in tools other than Bash, non-Hera Argus MCP tools other than `task_message_send`, reviewed connector reads and draft creation/update SHALL run without a prompt; the resident-coordinator specification’s structural Hera allow/ask/deny tiers SHALL remain unchanged; outward tools (every write/send/delete/modify tool of the outward connectors (Notion, Gmail, Superhuman, Slack, Linear, Google Calendar, Granola, Keystone, Figma, Sketch, Claude Docs and artifacts), except draft creation/update) SHALL require an explicit user confirmation, deferred through the resident approval board for supported PermissionRequest events; Slack message sending (including scheduled sends) SHALL be denied, with draft creation/update (such as `slack_send_message_draft`) allowed; Slack send and schedule are denied. Rules SHALL follow deny > ask > allow precedence. Connector allow rules SHALL name a literal server and reviewed read or draft tool, never a whole external server or global MCP allow wildcard. The resident SHALL request `auto` mode in both argv and inline settings, independently of `defaults.permission_mode`. Unmatched tools, including Bash, SHALL be reviewed by Claude Code’s auto classifier. If auto is unavailable because of settings, model, plan/account or classifier availability, Claude Code SHALL fall back to Manual (`default`) approval without preventing startup; Fallback diagnostics SHALL be best-effort (notice wording may change), match small core phrases with cursor movements treated as word separators, and go only to the supervisor log. Observation SHALL stop after the first 64 KiB of PTY output or 60 seconds from launch, whichever comes first; the retained 4 KiB text SHALL preserve UTF-8 rune and escape-sequence boundaries. No resident permission-mode configuration knob SHALL be exposed. An unmatched tool SHALL require confirmation after fallback to default mode. Because Claude Code does not support global MCP allow patterns or expose connector schemas to Argus at launch, reads from wholly unlisted servers MAY still prompt. A supported pending confirmation SHALL create a durable resident approval decision while Solo continues other work, and SHALL NOT raise the resident's waiting (`?`) signal. Unsupported native sandbox network and mandatory connector-consent prompts SHALL retain native waiting behavior.

The terminal missing-verdict stop SHALL raise needs-input for every Claude Code task, including non-residents; only the resident uses the resident-specific waiting flag.

The resident launch SHALL strip `--dangerously-skip-permissions`, `--allow-dangerously-skip-permissions` and `--permission-mode` (with its value) from the backend command, and SHALL then apply a flag allowlist to what remains: the executable SHALL be a Claude binary (any path whose base name is `claude`) and every other word SHALL be `--model`, `--effort`, `--fallback-model` (each with a value, space or `=` form) or `--verbose`, free of shell metacharacters. The executable word SHALL be a plain, unquoted path (no quotes, globs, escapes or `=`, and not starting with `-`, `+` or `%`) and MAY lead with `~/`; a value MAY be a simple single-quoted string (`--model 'opus[1m]'`, `--effort='high'`). Unescaped newlines or carriage returns (a newline ends the command in sh), double quotes, `$`, backticks, `;`, `|`, `&`, redirects, spaces, unbalanced quotes and unquoted `[`/`]` (a shell glob; the error suggests single-quoting) SHALL be refused. Any other word (including `--allowedTools`/`--allowed-tools`, `--plugin-dir`, `--plugin-url`, `--agent`, `--agents`, `--mcp-config`, `--settings`, `--setting-sources`, `--add-dir`, positional words and shell operators) SHALL make the launch fail with an error naming it, surfaced as the resident's reason; plain tasks are unaffected.

The policy SHALL be self-contained: the resident SHALL launch with setting sources restricted to `project` so the user's own settings (allow rules, hooks, plugins, model) do not apply, and only the resident's own allow list SHALL supply explicit approvals; auto mode MAY approve unmatched actions after classifier review. The user's `permissions.deny` and `permissions.ask` rules SHALL be merged into the resident's lists (never the user's `allow` rules), its inline settings SHALL set `permissions.defaultMode` to `auto` and disable bypass mode, SHALL carry the account's `model` and `effortLevel` when no other model is chosen and the account's `sandbox` settings block by allowlist (only `enabled`, `failIfUnavailable`, `network.deniedDomains`, `filesystem.denyRead` and `filesystem.denyWrite` are carried; every other key, including unknown ones and `credentials.*`, is dropped; nothing carried when none remains) with its Bash auto-approve and unsandboxed-command escapes forced off, except that `enabled` and `failIfUnavailable` SHALL NOT be carried when the resident itself runs under Argus's sandbox (user hooks are never carried; skipped hook events are logged), and SHALL register the `argus coord-hook` Stop hook and an authenticated resident PermissionRequest hook inline in place of dropped user hooks. The account's `CLAUDE.md` and user `skills/` SHALL still be provided to the resident. When Argus's sandbox applies to the resident but its profile cannot be generated, the launch SHALL fail with an error rather than start without either sandbox; plain tasks keep falling through unsandboxed. Sending a message to another task (`task_message_send`) SHALL require confirmation, while reading and acknowledging the resident's own inbox SHALL NOT.

#### Scenario: Calendar invite asks first

- **WHEN** the resident attempts to create a calendar event
- **THEN** the call is queued as an exact approval decision without raising waiting, and Solo continues other work

#### Scenario: Slack send is denied, draft allowed

- **WHEN** the resident attempts `slack_send_message`, then `slack_send_message_draft`
- **THEN** the first is denied and the second proceeds without a prompt

#### Scenario: User allow rules do not widen the resident

- **WHEN** the user's settings allow every tool of a connector the resident does not list
- **THEN** the resident's launch carries none of those allow rules and the tool goes through classifier review (or prompts after fallback to default)

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

#### Scenario: Bash goes through safety review

- **WHEN** the resident invokes Bash for ordinary work with no matching user ask/deny rule
- **THEN** no blanket Bash allow is shipped, narrow read-only grants can run immediately, and other commands receive auto review (or ask after Manual fallback); Argus sandbox restrictions still apply

#### Scenario: Narrow read-only shell grants preserve ordinary work

- **WHEN** the resident invokes ls, cat, head, tail, grep, wc or git status, or the exact commands rg --files, find . -type f, git log, git diff, git show or git branch
- **THEN** the shipped allow list supplies narrow grants without blanket rg/find/git execution permission
- **WHEN** the resident invokes git push, gh, find with -exec/-delete, rg with --pre, or git branch with mutation options
- **THEN** no shipped Bash allow matches and the action reaches auto review (or asks after fallback)

#### Scenario: Previously unlisted connector read runs without a prompt

- **WHEN** the resident invokes Keystone `approved_mcp_servers`, `drata_policies`, `list_directory`, `list_repositories` or `search_code`, absent from the old defaults
- **THEN** the shipped policy allows the read without confirmation

#### Scenario: Mail send asks

- **WHEN** the resident sends, replies to or forwards mail
- **THEN** confirmation is required on the approval board without raising terminal waiting

#### Scenario: Calendar event create asks

- **WHEN** the resident calls `create_event`
- **THEN** confirmation is required even when a broader allow also matches

#### Scenario: Slack draft is allowed

- **WHEN** the resident calls `slack_send_message_draft`
- **THEN** it runs without confirmation

#### Scenario: Slack send is denied

- **WHEN** the resident sends or schedules a Slack message
- **THEN** it is denied

#### Scenario: Unknown tool receives safety review

- **WHEN** the resident calls a new connector tool with no matching read/draft allow rule
- **THEN** it receives auto-mode safety review, or asks after fallback to default, including on an otherwise covered server

#### Scenario: User restrictions override broad allow

- **WHEN** the user asks for a Bash command or denies a connector read which the shipped policy allows
- **THEN** ask/deny remains effective, with deny taking precedence

#### Scenario: Resident strips bypass and requests auto

- **WHEN** backend flags or global defaults request bypass, plan or default mode
- **THEN** the resident strips backend permission flags and requests auto with bypass disabled and the inline allow/ask/deny policy intact

#### Scenario: Auto unavailable still starts

- **WHEN** Claude Code cannot enable auto for the current settings, account, model or service
- **THEN** it starts in default mode and Argus best-effort logs a matching startup notice once per session to the supervisor log; notice wording may change

#### Scenario: Fallback diagnostics stop after startup

- **WHEN** 64 KiB of PTY output has been observed or 60 seconds have passed since launch
- **THEN** the fallback observer stops parsing and releases its retained text and parser
- **AND** a notice after that window does not produce a fallback log

#### Scenario: Supported classifier escalation becomes a board decision

- **WHEN** auto mode escalates repeated blocks or an oversized classifier context to a permission chooser
- **THEN** a supported PermissionRequest is queued for human approval and the model continues; unsupported consent prompts still raise waiting

#### Scenario: Missing-verdict stop raises waiting

- **WHEN** Claude stops after repeated missing safety verdicts and asks the user to send a message or change mode
- **THEN** the idle task raises needs-input even though no numbered chooser or trailing question is present; this detection SHALL apply to every Claude Code task, both resident and non-resident

#### Scenario: Live bridge preserves terminal auto failure

- **WHEN** the resident stops after repeated missing verdicts while a live bridge reports no dialog (whether its last turn report is busy or idle)
- **THEN** the corroborated terminal stop still raises resident waiting and the daemon-resolved bridge snapshot exposes needs-input to every frontend
- **AND** a replacement launch SHALL NOT inherit the prior launch's stop marker, even when it resumes the same transcript

#### Scenario: Ordinary classifier denial does not raise waiting

- **WHEN** auto mode rejects one action or retries a transient classifier failure while Claude continues working
- **THEN** that notice alone does not raise waiting

