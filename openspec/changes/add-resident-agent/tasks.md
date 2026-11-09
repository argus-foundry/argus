**Design doc:** `openspec/changes/add-resident-agent/design.md`

Every stage follows TDD (write the failing test first), adds `[resident]`-prefixed uxlog calls, and keeps `make pre-pr` green. Tests never touch real `~/.argus/` or the live daemon.

## 1. Failing tests from the deltas

- [ ] 1.1 Write failing tests for each scenario in `specs/resident-agent/spec.md`, `specs/resident-tui/spec.md`, `specs/keybindings/spec.md`, `specs/coordinator-context-management/spec.md` and `specs/task-recycle/spec.md`
- [x] 1.2 Write `TestResidentExcludedEverywhere`, a table test listing every task enumeration, sweeper and lifecycle choke point against a seeded resident row
- [ ] 1.3 Confirm each `it should` criterion in design.md has a failing test (Prove-It Pattern)

Stage 6's slice of 1.1/1.3 is covered by `coord_hook_resident_test.go` and `TestResidentRecycleSeedOrder` (verified failing before implementation), with real metadata round-trip, sticky waiting, combined unread/waiting, and persistent-orientation recycle regression tests.

## 2. Resident entity and exclusion

**Depends on:** Stage 1

- [x] 2.1 Add `Task.Kind`, the `tasks.kind` column (default `""`), the partial unique index, `IsResident()`, and singleton create/lookup helpers in `internal/db`
- [x] 2.2 Relax the `BuildCmd` worktree precondition for a resident-kind task whose `Worktree` equals the resident working directory; verify `resume.go` and `bgsessionreap.go` need no change (Chesterton's Fence: worktree safety exists to keep agents out of project dirs)
- [x] 2.3 Apply the predicate at DB list queries for user-facing views and counts, then at each sweeper, reaper, PR poller, merge-safety, reconciliation, auto-prune and orphan-sweep site
- [x] 2.4 Reject complete, archive, delete, fork, set-status and account-move for the resident at DB and entry-point level (TUI, REST, MCP)
- [x] 2.5 Exclude the resident from REST `/api/tasks*`, MCP `task_list`/`task_get` and task SSE; publish `resident.*` events instead
- [x] 2.6 Make `TestResidentExcludedEverywhere` pass

## 3. Keymap

**Depends on:** Stage 1

- [x] 3.1 Extend `keymap.Parse` and `String()` for `ctrl+\` (`tcell.KeyCtrlBackslash`) with a pinning test
- [x] 3.2 Add `global.resident` (`ctrl+\`) and `global.tab_resident` (`0`) to `defaultSpecs`, `actionLabels`, `contextOrder`; add `help_test.go` assertions (`TestHelpModal_Draw`); verify no default conflict
- [x] 3.3 Mirror both rows into the README Reference keybinding table and add the iTerm2/Ghostty Cmd+` recipe plus the macOS window-cycling note
- [x] 3.4 Update `gotchas/keybindings.md`

## 4. Config and daemon lifecycle

**Depends on:** Stage 2

- [x] 4.1 Add the `[resident]` config block (defaults, name validation with no default name, `config.toml` override layer) and the KB/vault prerequisite check
- [x] 4.2 Implement the daemon ensure-running loop: start on supervisor re-attach, on setting flip, and on unexpected exit with bounded backoff and `--resume`; stop on disable; create `~/.argus/resident/`
- [x] 4.3 Resolve backend (default Claude-family), model and account overrides, and sandbox through the existing resolvers
- [x] 4.4 Add resident RPCs (state read, mark-viewed, send) and a minimal `ResidentSend` that reuses `notify.Notifier` reliable delivery (the routines seam)

## 5. Memory, orientation and permissions

**Depends on:** Stage 4

- [ ] 5.1 Write the resident orientation prompt: open-loops first, vault layout and `dream: skip` frontmatter, KB schema rules (search before write), outward-action confirmation rule, recycle-with-no-pending-work rule
- [ ] 5.2 Seed `resident/` working files (`open-loops.md`, `profile.md`, `preferences.md`) through the KB path on first start if absent, never overwriting
- [ ] 5.3 Deliver the three-tier permission policy through the backend family's permission mechanism (see `context/knowledge/backends.md`), with `ask_tools`/`deny_tools` defaults; confirm a pending confirmation raises the needs-input signal

## 6. Context self-management

**Depends on:** Stage 4

- [x] 6.1 Widen the coord-hook gate to the resident task (stamp, nudge variant, growth gate, no hard-stop, no role requirement) with hera behavior unchanged
- [x] 6.2 Record `last_reply`/`last_reply_at` from the Stop payload with sanitize and 200-rune truncation
- [x] 6.3 Add the resident recycle seed ordering to the `task_recycle` seed builder; plain tasks unchanged
- [x] 6.4 Compute the resident waiting signal with the existing detector and expose unread/waiting as one state

## 7. Tab and toggle

**Depends on:** Stages 3, 4

- [x] 7.1 Add the resident tab, header label from `resident.name`, `0` selection, and the single full-width resident pane
- [x] 7.2 Generalize `agentCrossReturn` into `residentReturn` (tab, mode, agent-view task and zoom, Hera focus via the existing excursion snapshot, Settings category/focus); record after `switchTab`
- [x] 7.3 Dispatch `global.resident` unconditionally in `CtxGlobal`, ignored in modal/text-input modes; `ctrl+q` on the resident tab returns; vanished-target fallback to Tasks; do not persist the tab
- [x] 7.4 SimulationScreen smoke tests for round trips from Tasks list, zoomed agent view, Hera pane and Settings, plus the modal-ignored case

## 8. Rail box and tab marker

**Depends on:** Stages 6, 7

- [x] 8.1 Add `ResidentBox` and stack it with `AttentionBar` in the agent-view left column, Hera rail summary and Tasks rail via the existing `OnHeightChange` flow
- [x] 8.2 Add the header tab-label marker (`●`/`?`) on every screen; clear on view via the mark-viewed RPC
- [x] 8.3 Smoke tests: both boxes visible together, waiting outranks unread, zero height when quiet, no PTY resize or kick when the box toggles, hidden on the resident tab

## 9. Settings

**Depends on:** Stage 4

- [ ] 9.1 Add the Resident Settings category (enabled, name, budget, memory dir, model, account, read-only tool-list pointer), local-only, with a required-name gate on enabling and prerequisite messaging
- [ ] 9.2 Settings smoke tests including live rename

## 10. Docs, gotchas, parity and archive

**Depends on:** Stages 2 through 9

- [ ] 10.1 Write `context/knowledge/gotchas/resident.md` (choke-point predicate, worktree precondition, hook gate, signals, rail-box no-resize invariant, hotkey recipe) and add rows to `context/knowledge/index.md`
- [ ] 10.2 Update README Reference tables (config keys, keybindings); leave the marketing half alone unless a new pillar-class claim is warranted
- [ ] 10.3 Record the named parity follow-ups (`add-resident-macos`, `add-resident-web`, routines, notifications, `add-resident-hard-stop`) and the ai-ron `airon-dream` skip rule in design.md Non-Goals
- [ ] 10.4 Run `make pre-pr` clean
- [ ] 10.5 Archive in the same PR before merge: `openspec archive add-resident-agent` (or merge deltas into base specs and move to `openspec/changes/archive/<date>-add-resident-agent/`) and commit
