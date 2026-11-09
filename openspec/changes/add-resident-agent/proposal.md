## Why

Argus can already see all of Aaron's work, memory (the Metis vault) and connectors, but every agent session is a disposable, worktree-bound task. There is no always-present assistant to ask "when's my next meeting with X?" or hand an errand ("book time with X"). The resident is that assistant: one long-lived, named agent that lives inside Argus, is not a task in any user-visible sense, learns by writing to the vault, and manages its own context window.

## What Changes

- Add a singleton **resident** agent: a task row with `kind=resident`, no git worktree (fixed working directory `~/.argus/resident/`), owned by the daemon and kept alive by the supervisor. It is never completed, archived, reaped or listed as a task.
- Every task enumeration, sweeper and status-driven machinery skips the resident via one `IsResident()` predicate (task list, jump switcher, REST/MCP task lists, finished-session reaper, devstack sweeper, orphan sweep, auto-prune, PR poller, merge-safety cleanup, status counts).
- Add a **first TUI tab** named after the agent (user-set in Settings, default "Solo"): `Solo | Tasks | Projects | Settings`. Digits `1/2/3` keep their meaning; `0` selects the resident tab.
- Add a user-definable **global toggle hotkey** (default `ctrl+\`, an unconditional global keymap action). Pressing it from anywhere opens the resident tab; pressing it again returns EXACTLY to the prior tab, mode, agent view and zoom. **BREAKING:** none, but `keymap.Parse` is extended to accept `ctrl+\`. A documented terminal recipe maps Cmd+` to byte 0x1c.
- Add a **resident box** in the left rail, stacked beside the needs-attention box (agent view left column, Hera rail, Tasks rail): a one-line reply preview with `●` (unread) or `?` (waiting on the user), zero height when nothing to show. On screens with no rail the resident tab label carries the same `●`/`?` marker.
- **Memory** is the main Metis vault. Working memory (open loops, profile, preferences, session log) lives in the fenced folder `resident/` (configurable `resident.memory_dir`, independent of the display name); durable facts go to normal topic notes under the KB schema. Every fresh session reads `resident/open-loops.md` first.
- **Self-managed context**: reuse the coord-hook `context_size` stamp and budget nudge (gate widened to the resident) and the existing role-free `task_recycle` primitive. The agent flushes its open-loops file and recycles itself at a quiet point; a resident-specific recycle seed makes the fresh session re-read the vault.
- **Outward actions** (sending invites or mail, replying to events) require a one-key confirmation using the Claude permission prompt, which also raises the `?` marker; Slack messages are drafts only.
- Settings gains a **Resident** category (enabled, name, context budget, memory dir, model/account).

## Capabilities

### New Capabilities

- `resident-agent`: the singleton resident task: identity, exclusion from task machinery, daemon-owned lifecycle, vault memory, context self-management, outward-action policy, reply/unread signal, configuration.
- `resident-tui`: the resident tab, global toggle with exact return, rail box and tab-label marker, Settings category.

### Modified Capabilities

- `keybindings`: new unconditional global action `global.resident`; `ctrl+\` keyspec; terminal-mapping recipe.
- `coordinator-context-management`: the Stop hook also acts for the resident task (budget nudge only; no hard-stop in phase 1).
- `task-recycle`: resident-specific recycle seed ordering (handoff note, then re-read open loops).

## Impact

- **Code:** `internal/model` (`Task.Kind`), `internal/db` (column, singleton index, `IsResident` filtering), `internal/agent` (worktree precondition relaxed for the resident working dir, resume/bgreap paths), `internal/daemon` (resident supervisor-of-one: ensure-running, reapers/sweepers skip), `internal/api` + `internal/mcp` (task enumerations skip), `cmd/argus` coord-hook (gate + nudge variant + reply capture), `internal/tui` (tab, rail box, toggle snapshot/restore, Settings), `internal/tui/keymap` (parse + action), `internal/config` (`[resident]` block), `internal/tui/widget` (header marker).
- **Docs:** README keybinding and config Reference tables, `?` help (generated), a new `gotchas/resident.md` plus index rows.
- **Cross-repo follow-up (ai-ron, not in this change):** the nightly `airon-dream` pass must skip `resident/**` and any note with `dream: skip`.
- **Parity:** TUI only in phase 1 (see Non-Goals in design.md for named web, macOS, mobile, routines and notification follow-ups).
