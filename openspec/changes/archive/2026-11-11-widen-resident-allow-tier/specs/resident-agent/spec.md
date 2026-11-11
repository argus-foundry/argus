## MODIFIED Requirements

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

