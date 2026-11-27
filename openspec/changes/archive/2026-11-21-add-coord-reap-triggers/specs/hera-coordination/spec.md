## MODIFIED Requirements

### Requirement: Coordinators proactively archive done-done roles

The coordinator orientation and the builtin `hera` skill SHALL present archive guidance as a short checklist tied to moments, not as a clause inside other prose. The checklist SHALL name these triggers:

- when a worker reports done and its PR is merged or open with CI not failing: call `hera_accept`, confirm the role is winding down, then call `hera_archive(role_name=...)` in the same turn
- on every status check or wake (`hera_status`, `hera_inbox`, handling a wake, including an `[argus]` "ready to archive" notice): sweep with `hera_archive(dry_run=true)` and archive whatever passes
- before going idle, and before reporting the orchestration finished: sweep with `hera_archive`

They SHALL also tell coordinators that:

- a worker whose PR is open with CI not failing, pushed, and with a clean worktree may be archived without waiting for the merge (a handed-off archive)
- `force=true` with a `reason` is the coordinator-only escape hatch, for a single named role, never used in a sweep
- `hera_unarchive` hands a role back
- a closed-unmerged PR of a handed-off role brings it back with a board decision item
- refusals are normal and carry reasons, and archive does not delete worktrees or branches

The orientation's archive guidance SHALL be its own block, separate from the opening dispatch paragraph.

#### Scenario: Orientation carries moment-based archive triggers

- **WHEN** the coordinator orientation is rendered
- **THEN** it contains an archive checklist naming the worker-done moment (with `hera_accept` then `hera_archive(role_name=...)`), the every-status-check `hera_archive(dry_run=true)` sweep, and the before-idle / before-finished sweep
- **AND** it states that `force` needs a named role and a reason and is never used in a sweep, and mentions handed-off archive and `hera_unarchive`

#### Scenario: Archive guidance is not buried in the dispatch paragraph

- **WHEN** the coordinator orientation is rendered
- **THEN** the archive checklist starts on its own line, after the paragraph that opens with the dispatch rule

#### Scenario: Skill names the same moments

- **WHEN** the builtin `hera` skill's `hera_archive` entry is read
- **THEN** it lists the worker-done, every-status-check and before-idle/finished moments, and says the daemon may send an `[argus]` ready-to-archive notice that the coordinator acts on with a sweep
