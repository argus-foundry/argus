## Context

- `hera_archive` (internal/mcp/hera.go → `hera.ArchiveRoleWith`) evaluates the done-done verdict on two paths: merged first, then handed off. It refuses a busy or prompt-parked session. With `dry_run=true` it reports what it would archive and writes nothing.
- The PR poller (`Daemon.pollPRStatesOnce`) already writes `pr` task_meta and sends the coordinator a "PR merged" nudge plus a self-assessment notice to the role. It does not tell the coordinator that a role is now archivable. A merged PR is also only one of the two archive paths.
- `hera_status(done)` rolls a worker task to `in_review` (BUG-050) but tells nobody it is archivable.
- Self-addressed hera messages (`from == to`) are a reserved system form. Board resolutions and resident approval notices insert them directly. `Service.Deliver` already renders a `[argus] `-prefixed self-addressed body as an `[argus] <tldr> · read: hera_inbox` doorbell line.
- Coordinators only see orientation text at spawn, so already-running coordinators can only be reached by a message.

## Goals / Non-Goals

**Goals:**

- Every coordinator, including ones already running, hears about its archivable roles promptly and without being asked.
- Reuse the existing verdict exactly. No second definition of "archivable".
- Bounded GitHub/git cost and no message spam.

**Non-Goals:**

- Auto-archiving. The coordinator still decides; the daemon only suggests.
- Nudging about merged-but-unaccepted workers (task `in_review`, PR merged). The verdict refuses those (the merged path needs `complete`, the handoff path needs an open PR). They are already covered by the existing PR-merge notices: the coordinator is told to review/accept, and the role is asked to `task_complete` itself. Once either happens the role becomes archivable and this nudge fires.
- A config knob or a Settings UI for the nudge. The behavior is on whenever hera is enabled. A kill switch is a possible follow-up if it proves noisy.
- Changing the archive verdict, the `hera_archive` tool surface, or any client.

## Decisions

### D1. Archivable = a dry-run `ArchiveRoleWith`, not a new predicate

The watcher calls `hera.ArchiveRoleWith(store, runner, git, role, ArchiveOptions{DryRun: true, Handoff: handoff})`, the exact call `hera_archive(dry_run=true)` makes, with the same production seams (`heraArchiver()`'s runner, `NewGitCheck`, `NewHandoffCheck`).

- `res.Archived && res.Kind` gives "archivable via merged / handed_off".
- The idle requirement is ArchiveRoleWith's own `busy()` gate: alive and not idle, or parked at a prompt, means refused. A dead session counts as idle, which is consistent with the tool.
- Every refusal reason, including lookup errors (fail closed inside the verdict), means no nudge.
- **Alternative rejected:** calling `ArchivableVerdict` + `HandoffVerdict` directly and re-checking idleness. That duplicates ArchiveRoleWith's ordering and busy gate, and invites drift.

### D2. Trigger mechanism: a debounced watcher, poked by existing events, plus a slow periodic sweep

A new `hera.ReapNudger` runs one goroutine with:

- **A poke channel** (non-blocking, coalescing). Pokes come from:
  - the PR poller, whenever it commits a changed `pr` state for a task (`wrote == true`)
  - `applyRoleStatus` when any role reports `done`
  - `toolHeraAccept` (the task flips to `complete`, which makes the merged path possible)
- **A debounce** (`reapPokeDebounce = 45s`) after a poke before the sweep runs. A worker reporting done is usually still mid-turn, and the debounce lets it finish and go idle, so the first look is usually conclusive. Bursts of pokes collapse into one sweep.
- **A periodic sweep** (`reapSweepInterval = 5m`) for transitions no event announces: CI going from pending to green, a session going idle after the debounce, a worker's unread outbox getting read.
- **Alternatives rejected:**
  - Doing the check inline in the PR poller or the MCP handler. The verdict shells out to `gh` and git (up to 20s each), which must not stall a poll cycle or an MCP call.
  - Purely periodic. Too slow after the common "worker reports done" moment unless the interval is aggressive, which costs GitHub API budget.
  - Purely event-driven. CI going green and a session going idle have no event.

### D3. Cost control: cheap pre-filter, then throttled verdict

Each sweep lists live orchestrators and their worker/freelance roles, then pre-filters on database reads only. It skips a role when any of these is true:

- archived, nuked, cancelled or pinned
- no live binding
- task not `in_review`/`complete`
- task archived, pinned or resident
- the session is alive and not idle

Only survivors run the dry-run verdict. Results are cached per role, keyed by a cheap fingerprint: task status, role status, cached `pr` state/lifecycle, and session alive/idle. A role whose fingerprint is unchanged is not re-evaluated for `reapReevalInterval = 15m`. This throttles both refusals (for example "CI failing") and passes. Each sweep is also capped by a time budget (`reapSweepBudget = 90s`); roles not reached wait for the next sweep. Steady state with nothing changing costs one database scan per 5 minutes plus at most one `gh pr view` per finished role per 15 minutes.

### D4. Dedup and batching

- **Persistent per-role record.** After a nudge, the watcher writes task_meta `hera/reap_nudged = "<kind>|<task status>|<pr state>|<unix>"`. A role is included in a nudge only when that record is missing, its kind/status/PR part differs from the current one (for example handed off → merged), or it is older than `reapRenudgeCooldown = 6h`. Persisting it means a daemon bounce, which happens often during development, does not re-announce everything. task_meta is cleared when the task is archived (`SetArchived`), so the record goes away with the role. It is written only when a nudge is sent, so it rarely bumps `data_version`.
- **Batching.** In one sweep, all archivable roles under the same coordinator go into ONE message.
- **Per-coordinator minimum gap.** The watcher sends at most one nudge per coordinator per `reapCoordGap = 10m`, tracked in memory. Roles found during the gap are not recorded as nudged, so the next sweep after the gap includes them. That sweep re-evaluates them only if their fingerprint changed or the re-eval interval passed. Otherwise it uses the cached pass.
- **Recipient.** The orchestrator's coordinator role, with a live binding. A coordinator with no live binding, or an archived one, gets nothing, and nothing is recorded, so it is told once it is back. Roles of a sub-coordinator's child orchestrator go to that child's coordinator.

### D5. Message form: a self-addressed `[argus]` system notice

- A new `db.SendHeraSystemNotice(roleID, body, tldr)` inserts a self-addressed message through the same validation as `SendHeraMessage`: caps, the inbox-full limit, and the archived-recipient check. The only difference is that self-send is allowed, and the body must start with `[argus] `. `Service.Deliver` already rings an `[argus] <tldr> · read: hera_inbox` doorbell for that form, idle-gated through the same notifier as worker reports.
- Body: `[argus] N role(s) ready to archive: a (handed off), b (merged). Run hera_archive(cwd=$PWD) to retire them; it re-checks each one, and a refusal just means it changed. The daemon never archives on its own.`
- tldr: `N role(s) ready to archive: a, b`, truncated with an ellipsis to fit 120 characters.
- `HeraMessage.SenderName` returns `argus` for a self-addressed body starting with `[argus] `, and keeps `Aaron (board)` for other self-addressed messages.
- **Alternative rejected:** sending from the archivable role to the coordinator, as the merge nudge does. A batched message has no single sender, and it would wrongly count as mail the role sent.

### D6. Prompt text: a checklist block, not a clause

In the orientation, the two archive sentences ("Retire done-done roles…" and "You do not have to wait for the merge…") move out of the first paragraph into their own short block before the five habits:

```
ARCHIVE FINISHED ROLES with hera_archive(cwd=$PWD, ...). It re-checks everything, refuses what isn't merged or handed off (PR open, CI not failing, pushed, clean), never deletes worktrees or branches; refusals are normal.
- A worker reports done and its PR is merged, or open with CI not failing: hera_accept, confirm it is winding down, then hera_archive(role_name=...) in the same turn.
- Every hera_status / hera_inbox check or wake (including an [argus] "ready to archive" notice): sweep with hera_archive(dry_run=true), then archive what passes.
- Before going idle, and before reporting the orchestration finished: sweep with hera_archive.
- force=true needs role_name and a reason, never in a sweep. A PR closed unmerged brings the role back with a board decision; hera_unarchive(role_name=...) hands one back.
```

The skill's `hera_archive` entry gets the same three moments as a "When to call it" sub-list, plus one line on the daemon nudge.

## Risks / Trade-offs

- [Nudge arrives, but the role changed before the coordinator acts] → The message says hera_archive re-checks everything and that a refusal is fine. The coordinator's sweep is authoritative.
- [GitHub budget from `gh pr view` per candidate] → Pre-filter plus the 15m throttle plus the sweep budget (D3). The poller's own batched GraphQL stays the main PR-state source.
- [Coordinator keeps ignoring the nudge] → Re-nudged at most every 6h per unchanged role, and at most once per 10m per coordinator.
- [A dead session counts as idle, so a crashed worker with a green PR gets nudged] → Consistent with `hera_archive`, which archives it. That is the desired reaping.
- [The `argus` label change also relabels the resident's existing `[argus]` approval notices] → Intended: those notices come from the daemon, not from Aaron.

## Migration Plan

No schema change. The task_meta key is additive. Rollback is reverting the PR; leftover `reap_nudged` keys are harmless.

## Open Questions

- None blocking. Possible follow-up: a `[hera] reap_nudge = false` kill switch, if real-world noise warrants one.
