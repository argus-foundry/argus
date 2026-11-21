## Why

On 2026-11-21, 58 of 62 running argus sessions were idle, holding about 35 GB of RAM. 33 of them were finished Sketch hera workers (PR open and green, or merged) that their coordinators never archived. `hera_archive` already handles this correctly when asked. Coordinators just don't call it on their own, because the guidance names no trigger moment. And prompt text only reaches coordinators spawned after an edit, so a prompt fix alone never reaches the ones already running.

## What Changes

- **Trigger-based archive instructions.** The coordinator orientation (`HeraCoordinatorOrientation`) and the builtin `hera` skill's `hera_archive` entry replace the mid-paragraph "should archive" clause with a short checklist tied to moments:
  - when a worker reports done and its PR is merged or open with CI not failing
  - on every status check or wake
  - before going idle or reporting the orchestration finished

  The orientation's length stays roughly flat: the archive text moves out of the run-on paragraph instead of being added to it.
- **Daemon nudge to the coordinator.** A new daemon watcher sends a coordinator ONE `[argus]` hera message listing its worker/freelance roles that just became archivable, for example "2 roles ready to archive: fqs-pr2 (handed off), fqs-pr3 (merged). Run hera_archive to retire them." The message goes through the normal hera inbox, so it rings the doorbell and wakes an idle coordinator.
  - "Archivable" means the existing verdict (merged or handed-off path) passes AND the session is idle or gone. The watcher evaluates it as a `hera_archive` dry run, so the logic is reused, not re-implemented. Lookup errors fail closed.
  - Triggers: a PR-state change written by the PR poller, a worker's `hera_status(done)`, and `hera_accept`. Each one pokes the watcher, which runs after a short debounce. A low-frequency periodic sweep catches everything else (CI turning green, a session going idle).
  - No spam: roles are batched into one message per coordinator, the watcher debounces, a per-coordinator minimum gap applies, and a role is not re-nudged unless its archive kind or task state changed, or a long cooldown passed. The nudged record survives a daemon bounce. Pinned and resident roles, coordinators, and standing-team members are never nudged (the verdict already refuses them).
  - The daemon only suggests: it never archives anything.
- **Sender label.** A self-addressed hera message whose body starts with `[argus] ` is labeled `argus` (not `Aaron (board)`) in `hera_inbox`, the TUI inbox viewer, and the REST message list. This also corrects the label on the resident's existing `[argus]` approval notices.

## Capabilities

### New Capabilities

<!-- none -->

### Modified Capabilities

- `hera-coordination`: "Coordinators proactively archive done-done roles" is rewritten as moment-based triggers (worker done, every status check, before idle or finish) and mentions the daemon nudge.
- `hera-auto-archive`: ADDED "Daemon nudges coordinators about archivable roles" (watcher, triggers, batching, dedup, fail-closed).
- `hera-messaging`: ADDED "System notices carry an argus sender label" (`[argus]` self-addressed messages render as sender `argus`).

## Impact

- **Code:**
  - `internal/hera/reap_nudge.go` (new watcher)
  - `internal/daemon/daemon.go` (wiring, plus a poke from the PR poller)
  - `internal/mcp/hera.go` (poke on `hera_status(done)` and `hera_accept`; poke setter on `mcp.Server`)
  - `internal/db` (system-notice insert helper; `SenderName` label)
  - `internal/agent/hera_spawn.go` (orientation)
  - `internal/skills/builtin/hera/SKILL.md`
- **Supervisor surface:** `SKILL.md` is in `SupervisorSpawnPaths`. This change re-records the digest only, because a guidance-text change does not change how a session is constructed (same precedent as earlier `hera` skill edits).
- **Frontends:** no new endpoint, field or tool. The nudge reaches the TUI, web and macOS through the existing hera message surfaces, and the `argus` sender label is a server-side string all three already display. No client changes and no README tool-table change.
- **GitHub API:** the full verdict runs `gh pr view` plus git for each candidate. The watcher only does this for roles that pass cheap database pre-filters, and throttles re-evaluation, so steady-state cost stays small (see design D3).
