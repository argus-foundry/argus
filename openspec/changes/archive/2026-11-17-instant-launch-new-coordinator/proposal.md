# Proposal: instant launch for a new root coordinator

## Why

Creating a task in the Tasks tab shows a LAUNCH screen the instant the form
closes and lands focus on the agent when creation finishes. Creating a new root
coordinator in the Projects (Hera) tab does not: after the operator submits the
new-coordinator form, nothing visible happens for several seconds (the
worktree + session create runs off-thread), the coordinator then appears in the
rail, and focus is not on it.

## What changes

- On submit, the rail immediately shows an optimistic **pending** coordinator
  row (a synthetic orchestrator overlay, never persisted), selects it, and
  moves focus to the coordinator pane, which shows the same LAUNCH banner the
  Tasks flow uses (the pane's `SetPending` banner).
- On success the pending row is replaced by the real orchestrator. If the
  pending row is still the selected row, selection and focus land on the real
  coordinator pane (already live). If the operator navigated elsewhere while it
  loaded, the rail refreshes but selection and focus are left alone.
- On failure the pending row is removed and the error is surfaced; no orphan
  rows.
- Mutation keys on the pending row are inert (it has no DB identity yet).

## Non-goals

- Web SPA and macOS app: new-coordinator creation is TUI-only over REST (a
  standing intentional gap, see AGENTS.md "Frontend Parity"); no web or macOS
  work. Neither client has a pending-row concept to mirror.
- No change to `SpawnHeraCoordinator`'s transactional behavior, naming, or
  persistence; the overlay lives only in the TUI.
- Worker spawn (`w`) keeps its existing flow.
