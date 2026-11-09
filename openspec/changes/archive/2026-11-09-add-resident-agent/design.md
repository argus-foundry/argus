## Context

Argus runs disposable, worktree-bound agent tasks. Aaron wants one always-present assistant (think a personal "Dot") that sits inside Argus, so it can see his work, the Metis vault and every connector the Claude Code account has (calendar, Granola, Slack, Notion, Linear, mail, the KB). He asks quick questions ("when's my next meeting with X") and gives errands ("book time with X"). It learns by writing to the vault, and manages its own context window the way a hera coordinator does.

Brainstorm decisions (settled with Aaron):

- The resident is a **task row with `kind=resident`**, hidden from every task view, with its own tab.
- Default toggle hotkey is **`ctrl+\`**, plus a documented terminal recipe so Cmd+` works.
- Context management is **agent-driven for now** (nudge, flush open loops, `task_recycle`). A daemon-forced hard limit is a named follow-up.
- Attention uses a **rail box beside the needs-attention box**, plus a marker on the tab label on screens with no rail.
- Working memory lives in a fixed vault folder, **`resident/`**, independent of the agent's display name.

Verified current state (from code and specs):

- `BuildCmd` refuses a task with an empty `Worktree` (`internal/agent/agent.go:1084`); `resume.go:34` and `bgsessionreap.go:55` also read it.
- The KB indexes exactly one vault, `KBConfig.MetisVaultPath` (`internal/config/config.go:254`). The `kb_*` MCP tools (`kb_search`, `kb_read`, `kb_ingest`, `kb_list`, `kb_delete`) write vault-relative paths through the daemon, so a sandboxed session needs no direct vault write grant.
- The `argus coord-hook` Stop hook no-ops unless the task has a hera role binding (`coordinator-context-management`). `task_recycle` (`task-recycle`) is role-free: it waits for idle, kills stray jobs, and restarts the same task row with a handoff note seeded before the original prompt.
- `keymap.Parse` accepts only `ctrl+<letter>`, bare runes, and `cmd|opt|alt|shift` combined with navigation keys (`internal/tui/keymap/parse.go`). Today's `cmd+…` is iTerm2's Ctrl+Alt (mod-7) mapping. `ctrl+\` is unused anywhere.
- `agentCrossReturn{taskID, zen}` is recorded AFTER `switchTab` (because `switchTab` clears it) and replayed by `returnToAgentView` (`gotchas/keybindings.md`). Hera has its own excursion snapshot for ctrl+g/ctrl+b.
- `AttentionBar` sits at the top of the agent-view left column with fixed height 0 until non-empty; `OnHeightChange` resizes only that column, never the PTY pane. Hera has the equivalent summary box on its rail.

## Goals / Non-Goals

**Goals:**

- A singleton resident that survives TUI restarts and daemon bounces, is invisible to task machinery, and is reachable in one keystroke from any screen.
- Exact return from the resident to wherever the user was.
- Memory in the existing Metis vault, with a fenced working folder and normal KB notes for durable facts.
- Reuse of `task_recycle`, the coord-hook `context_size` stamp, needs-input detection, `task_meta`, the keymap and the supervisor, with no forked copies.
- A design that a second frontend (macOS, web) can drive later with no daemon change, because the daemon owns the session and every frontend is a client.

**Non-Goals (each a named follow-up, per the AGENTS.md Frontend Parity rule):**

- **macOS app:** global hotkey and floating panel. Follow-up: `add-resident-macos`.
- **Web/PWA and mobile:** a resident tab, then a native app distributed manually. Follow-up: `add-resident-web`. In phase 1 the resident is excluded from `/api/tasks`, so web and macOS simply do not see it.
- **Routines and scheduling:** "notify me every Thursday to take out the trash". Next phase after this one. See "Routines seam" below so this change does not design it into a corner.
- **Push and system notifications** for resident replies (arrives with routines).
- **Daemon-forced hard-stop recycle** at a higher budget multiple (the coordinator path has one at 1.5×; the resident gets it only if agent-driven recycling proves insufficient). Follow-up: `add-resident-hard-stop`.
- **Cross-repo:** `airon-dream` in ai-ron must skip `resident/**` and `dream: skip` notes. Until it does, the folder is unfenced, and this change says so rather than claiming protection.
- Multiple residents, non-Claude backends, and a REST read surface for the resident.
- **Marking every Argus notice in last-exchange extraction.** This change makes `[argus] `-prefixed turns (the resident's nudge, task-message doorbells, the resident's orientation and recycle seed; typed or bridge-delivered) never count as the user's last message, and stops attributing later replies to the earlier human. Unprefixed Argus notices are not covered: bridge-delivered turns without the prefix (`[argus-delivery:…]`, the argus-bridge plugin prefix) are skipped as the human message but still let later replies attach to it, and typed hera doorbells (`[hera from …]`) and `/api/notify` texts can still be quoted as the human message. Fixing either changes master's hera recycle seeds, so it is out of scope. Follow-up: `widen-last-exchange-delivery-tags`.

**Named follow-ups recorded at ship time (Stage 10):** `add-resident-macos` (global hotkey, floating panel), `add-resident-web` (resident tab for the PWA and a REST read surface), routines (scheduled notifications such as the Thursday trash reminder), push/system notifications for resident replies (arrive with routines) `add-resident-hard-stop` (daemon-forced recycle) and `widen-last-exchange-delivery-tags` (mark every Argus notice — unprefixed bridge-delivered turns, hera doorbells, `/api/notify` texts — in recycle-seed extraction). The macOS and web clients stay blind to the resident until those land; this is an intentional parity gap, not an oversight. The ai-ron `airon-dream` skip rule (skip `resident/**` and `dream: skip` notes) is a cross-repo item this change cannot ship: until it lands, the memory folder is unfenced from the nightly dream pass. Also by design, a `config.toml` `[resident]` value overrides Settings edits without a warning.

## Decisions

### D1. The resident is a task row with a `kind` marker

`Task.Kind` is `""` for normal tasks and `"resident"` for the resident. A partial unique index enforces at most one resident. The row is created lazily by the daemon the first time the resident is enabled, never by `task_create`, the TUI, REST, or `schedule_*`.

Fields: `Name="resident"` (fixed; the display name is config), `Project=""`, `Branch=""`, `Worktree=~/.argus/resident/` (a plain directory the daemon creates, NOT a git worktree), `Status=in_progress` permanently.

Alternatives considered:

- *Own entity and table.* Nothing leaks into task views, but session handling (runner, supervisor RPC, REST, SSE, bridge, needs-input) is keyed by task ID throughout, so this duplicates or generalizes a very large surface.
- *Task row plus a fake hera binding.* Gets the recycle watcher for free but puts a fake orchestrator on the Hera rail. Rejected; `task_recycle` is already role-free, so the trigger is the only missing piece.

Chesterton's Fence: the worktree precondition exists so agents never run in the project directory. The resident has no project, so the precondition becomes "a worktree OR a resident working directory that is exactly the configured resident dir". `Worktree` is therefore non-empty for the resident, which keeps `resume.go` and `bgsessionreap.go` working with no change. All worktree creation, cleanup, orphan sweep and path-validation choke points already operate under the managed worktree root, which `~/.argus/resident/` is outside, so they ignore it.

### D2. One predicate, one test that enumerates the choke points

`model.Task.IsResident()` is the only way code asks the question. It is applied at:

- DB list queries used for user-facing task views and counts (so every client gets it for free).
- Status-driven machinery: reconciliation roll-ups, idle to in_review promotion, PR poller, merge-safety cleanup, `RollHeraWorkerToReview` family.
- Sweepers: finished-session reaper, devstack sweeper, orphan sweep, auto-prune, host-suspend advisory.
- Lifecycle verbs: complete, archive, delete, status set, fork, account move, task_create-with-override, and the TUI/REST/MCP entry points for them reject the resident with a clear error.
- Surfaces: task list, jump switcher, Hera roster, REST `/api/tasks*`, MCP `task_list`/`task_get` (the resident is invisible there; its own session is addressed by the resident RPCs), SSE task events (resident events are published under a distinct `resident.*` event type).

A single table-driven test (`TestResidentExcludedEverywhere`) lists every choke point so a future task enumeration cannot silently include the resident. This is the main risk of D1, so it is paid for with a test and a gotcha entry.

### D3. Daemon-owned lifecycle

When `resident.enabled` is true the daemon ensures the session is running: after supervisor re-attach at daemon start, when the setting flips on, and (with backoff) when the session dies unexpectedly. It resumes the existing Claude session (`--resume`) rather than starting empty, except after an intentional recycle. When disabled the daemon stops the session; the row and the vault memory stay. Runs in the supervisor like any agent PTY, so daemon bounces re-attach and only a supervisor restart ends it.

It is not sandbox-exempt: it runs under the normal sandbox config (`Sandboxed` resolved at creation) and uses the `kb_*` MCP tools for all vault I/O, so it needs no vault write grant. Connectors come from the Claude Code account's global MCP configuration plus the Argus MCP injection every task already gets; nothing is wired per connector.

Backend: the default Claude-family backend only (the Stop hook and `task_recycle` are Claude-hook based). `resident.model` and `resident.account` are optional overrides resolved through the existing model and account resolution.

### D4. Memory in the Metis vault

- **Working memory** in `resident/` (config `resident.memory_dir`, vault-relative, default `resident`): `open-loops.md` (anything undone), `profile.md` (a profile of Aaron), `preferences.md`, and `log/YYYY-MM-DD.md` (session log). Each carries frontmatter `dream: skip` plus the KB-required `title` and `tags`.
- **Durable facts** (a person's role, a customer detail, a decision) go into the ordinary topic notes following the KB schema in the Argus MCP instructions: `kb_search` first, update rather than duplicate, kebab-case paths, wikilinks.
- **Open-loops first:** the resident's orientation prompt and every recycle seed instruct it to `kb_read` `resident/open-loops.md` before anything else.
- The folder name is fixed so renaming the agent in Settings never touches memory (and Argus rules forbid migration code).
- The `dream: skip` marker fails safe once `airon-dream` honors it (cross-repo follow-up).
- **Name prerequisite:** the resident has no default name. Naming it is the act of opting in, so `resident.enabled` cannot take effect without a valid `resident.name`; Settings asks for the name before enabling and an enabled-but-unnamed config does not start.
- **KB prerequisite:** the resident requires `kb.enabled` and a resolvable `metis_vault_path`. If absent, Settings shows why and the resident does not start. Running a memory-less resident would silently violate "it learns".

### D5. Context self-management (agent-driven)

- The coord-hook Stop hook gate widens from "hera role bound" to "hera role bound OR the task is the resident". For the resident it stamps `context_size` unconditionally (existing behavior) and, when `context_size` is at or above `resident.context_budget`, emits the existing non-blocking nudge variant. The nudge tells the agent to (1) write undone items to `resident/open-loops.md`, (2) call `task_recycle` with a short `handoff_note`, and (3) not do so while it has background work or a turn it still owes the user.
- Throttling reuses the existing growth-based increment gate (a nudge recurs only after `context_size` has grown by the configured increment).
- **No hard-stop in phase 1.** The hera 1.5× forced recycle is coordinator-only and stays so.
- `task_recycle` already guarantees "never mid-turn" (it waits for genuine idleness, bounded) and kills stray background jobs. The "no background work in flight" rule is therefore a prompt-level instruction plus the primitive's stray-job cleanup as the safety net, not a new daemon check.
- The recycle seed for the resident orders: handoff note, then "read `resident/open-loops.md`, then `profile.md`, then continue", then the persistent orientation as background. It needs zero follow-up tool calls beyond those reads.

### D6. Outward actions need one-key confirmation

The resident's permission policy is expressed through the backend family's existing permission-delivery mechanism (see `context/knowledge/backends.md`) with three tiers:

- *Read-only* connector tools (calendar list/search, meeting search, mail read, Slack read, Notion/Linear read, `kb_*`): allowed without a prompt.
- *Outward* tools (create/update/delete calendar events, respond to events, send mail or drafts, create Slack canvases or lists): ask. Claude's permission prompt is the one-key confirmation, and it is detected by the existing needs-input detector, which raises the `?` marker.
- *Slack sending*: `slack_send_message` and scheduled sends are denied outright; only `slack_send_message_draft` is allowed. This matches the existing drafts-only rule.

The tier lists are defaults in config (`resident.allow_tools`, `resident.ask_tools`, `resident.deny_tools`) so new connectors can be added without code. The resident always launches in `--permission-mode default` (bypass flags stripped from its command) because that mode is the version-independent one for ask enforcement; in it anything unlisted prompts, so the read-only tier is an explicit allow list. A headless check on claude 2.1.317 showed ask and deny held under bypass too, so this is a conservative choice, not a workaround.

### D7. Reply and unread signal

The coord-hook, already running on every resident Stop, records `task_meta(resident, last_reply)` (first line of the Stop payload's `last_assistant_message`, control characters stripped, truncated to 200 runes) and `last_reply_at`. `last_viewed_at` is set by an RPC the resident tab calls when the pane is shown and focused. **Unread** = `last_reply_at > last_viewed_at`. **Waiting** = the existing sticky needs-input signal for the resident task (the resident is excluded from the generic `needsInputIDs` task sets, so the resident's flag is computed by the same detector and exposed as its own field). State lives in `task_meta`, so any later frontend reads the same truth.

### D8. TUI surfaces

- **Tab:** first position, labelled with `resident.name` (no default; required to enable the resident; 1 to 12 printable runes, validated). `1/2/3` keep selecting Tasks/Projects/Settings; new global action `global.tab_resident` defaults to `0`. Not persisted by "restore last tab" (a launch that would restore it lands on Tasks).
- **Pane:** one full-width terminal pane bound to the resident session via the existing `TerminalPane`; typing goes straight to the PTY.
- **Toggle (`global.resident`, default `ctrl+\`)**: unconditional `CtxGlobal` dispatch like ctrl+k/ctrl+j, ignored only while a modal or text-input mode is open. From any non-resident screen it records a `residentReturn` snapshot (the generalization of `agentCrossReturn`: tab, mode, agent-view task and zoom, Hera selection/focus/fullscreen via the existing Hera excursion snapshot, Settings category/focus) and opens the resident tab; on the resident tab it restores the snapshot, falling back to the Tasks list if the target vanished or no snapshot exists. `ctrl+q` on the resident tab behaves the same as the toggle. Leaving the tab by any other route discards the snapshot (any other navigation forgets the hop, same as `agentCrossReturn`).
- **Rail box (`ResidentBox`)** stacked with `AttentionBar` in the agent-view left column, the Hera rail summary area and the Tasks rail. Fixed height 0 when read and not waiting; otherwise one row: `Name ● <preview>` or `Name ? <preview>`. It resizes only the left column through the same `OnHeightChange` flow, so PTY panes are never resized. It hides while the resident tab itself is showing.
- **Tab-label marker:** `Solo ●` / `Solo ?` in the header on every screen. This is the only signal on screens with no left column (plain Tasks list, Settings).
- **Settings:** a Resident category (enabled, name, context budget, memory dir, model, account, confirm/deny lists read-only pointer to config.toml). Local-only edits, like Accounts.

### D9. Terminal hotkey recipe

A TUI cannot see Cmd. macOS also intercepts Cmd+` for window cycling unless the user disables it. So the default is the control byte `0x1c` (`ctrl+\`), which reaches the TUI in any terminal. `keymap.Parse` gains `ctrl+\`, mapped to `tcell.KeyCtrlBackslash` with a pinning test (same discipline as `ctrlKeys`). The README documents the recipe: in iTerm2, Keys then Key Bindings, map Cmd+` to "Send Hex Code" `0x1c`; in Ghostty, `keybind = super+grave_accent=text:\x1c`; and turn off the macOS "Move focus to next window" shortcut so the chord reaches the terminal. Users can rebind to any representable spec via `[keybindings]`.

### Routines seam (so the next phase is not designed into a corner)

Routines will need (a) a schedule whose delivery target is a message to the resident rather than a new task, and (b) push notifications. This change deliberately leaves two seams open: the resident is addressable by a daemon-side `ResidentSend(text)` that reuses `notify.Notifier` reliable delivery (the same idle-gated, exactly-once bus Hera uses), and reply state lives in `task_meta` rather than TUI memory. The scheduler change then adds a target kind; nothing here needs to change.

## Alternatives considered

1. **Resident as a task row (chosen) vs own entity vs task plus fake binding:** see D1.
2. **Attention: tab badge, visor line, rail box (chosen).** A permanent top "visor" row was rejected by Aaron; a status-bar-only notice was rejected as easy to miss. The rail box reuses the proven `AttentionBar` mechanics and does not resize PTY panes.
3. **Hotkey default:** `ctrl+\` (chosen), `ctrl+space` (macOS input-source conflict, needs a Parse extension too), existing `ctrl+<letter>` such as `ctrl+e` (steals a composer editing key).
4. **Context trigger:** agent-driven (chosen for phase 1), daemon-driven, or both. Hybrid is the eventual target; deferred.
5. **Memory folder:** fixed `resident/` (chosen) vs named after the agent (rename becomes a migration, which Argus forbids).

## Risks / Trade-offs

- **[A task-shaped resident leaks into some task view or sweeper]** → one `IsResident()` predicate, applied at DB list level where possible, plus an enumerating test and a gotcha.
- **[Code assuming non-empty `Project`/`Branch` or a git worktree panics on the resident]** → the enumerating test runs every choke point against a resident row; `Worktree` is non-empty by design.
- **[Agent ignores the budget nudge and context grows unbounded]** → accepted for phase 1 (Aaron chose agent-driven); `resident.context_budget` is visible and `add-resident-hard-stop` is the named follow-up. The Stop hook still stamps `context_size`, so the problem is observable.
- **[`dream` pass edits or archives working memory]** → `dream: skip` marker and fixed folder; unfenced until ai-ron honors it (documented).
- **[Preview text exposes private content in the rail on a shared screen]** → first line only, 200 runes max, control characters stripped; a preview-off setting is not added now (YAGNI) but the state shape makes it trivial.
- **[`ctrl+\` byte 0x1c could signal the foreground process in cooked mode]** → Argus runs the TUI in raw mode, and the key is consumed by Argus before it reaches any agent PTY.
- **[Resident prompt gives it broad connector reach]** → the three-tier permission policy (D6), Slack send denied, and an orientation rule to confirm intent before outward actions.
- **[Recycle while background work is in flight]** → the primitive kills stray jobs; the prompt tells the agent not to recycle with work pending; the open-loops file is the recovery path.

## Migration Plan

- Additive schema: `tasks.kind` column (default `""`) and a partial unique index. Per AGENTS.md there is no legacy migration code; a one-off script is not needed because the default is correct for every existing row.
- Ship disabled (`resident.enabled=false`). Enabling is a Settings toggle; disabling stops the session and leaves the row and vault folder intact.
- Rollback: set `resident.enabled=false`. Removing the feature leaves one inert row and a vault folder that is Aaron's to keep or delete.

## Acceptance criteria

Resident entity and lifecycle:

- it should create exactly one resident row on first enable and never allow a second
- it should reject complete, archive, delete, fork and status changes on the resident
- it should appear in no task list, jump switcher, REST/MCP task list, count, sweeper or reaper
- it should keep its session across TUI exit and daemon bounce, and resume it after a crash
- it should stop its session when disabled and leave the row and vault folder alone
- it should refuse to start when the KB or vault path is unavailable and say why

Memory:

- it should read `resident/open-loops.md` first on every fresh session and recycle
- it should write working memory only under `resident/` with `dream: skip` frontmatter
- it should put durable facts in topic notes after a `kb_search`

Context:

- it should stamp `context_size` on every resident Stop
- it should nudge once the budget is reached and again only after the configured growth
- it should recycle only at idle, killing stray jobs, and seed the new session with handoff then open loops
- it should do nothing hera-specific for the resident (no binding, no hard-stop)

Outward actions:

- it should ask before any outward connector call and surface `?` while waiting
- it should deny Slack message sends and allow drafts

TUI:

- it should show the resident tab first with the configured name and keep `1/2/3` unchanged
- it should return exactly to the prior tab, mode, agent-view task, zoom and Hera focus on the second toggle
- it should ignore the toggle while a modal or text input is open
- it should show an unread `●` or waiting `?` in the rail box beside needs-attention, with zero height otherwise, without resizing any PTY pane
- it should show the same marker on the tab label on every screen
- it should accept `ctrl+\` as a keyspec and let the user rebind the toggle

## Open Questions

None blocking. Confirm at review (defaults chosen by the author, easy to change): `0` as the resident tab digit; default `resident.enabled=false`; Claude-family only; the preview source being the Stop-hook `last_assistant_message`; the exact default `resident.context_budget` (initially the coordinator default).
