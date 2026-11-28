## MODIFIED Requirements

### Requirement: Deleting a worker is merge-safe by default and forceable

Unless `force` is true, `hera_delete_worker` SHALL first classify the worker's work with the merge-safety classifier, built through one shared single-task helper that uses the same candidate construction as the cleanup pass, and SHALL proceed only when the working tree is clean and either the verdict is safe or the work is empty; a dirty working tree SHALL refuse without `force` even when the verdict is safe. Without `force` it SHALL also refuse, changing nothing, unless the worker's session is idle or absent ("worker is busy; wait, or force"), and it SHALL proceed in this order: idle check, clean-tree check, classification, stop the session, re-check the tree, then nuke the role and remove the worktree. If the post-stop re-check finds the tree dirty, or the worker's branch tip has moved since classification, it SHALL refuse before nuking, keeping the role, binding and worktree and naming the dirty files; this is the one refusal that leaves a side effect, the stopped session, which a later resume recovers. The whole call, including time spent waiting for any other resident worker mutation, SHALL run under one overall deadline that keeps it inside the MCP client deadline; if before the stop the remaining budget cannot cover the rest, it SHALL refuse with "host busy; retry" and change nothing. Deleting the worker's remote branch SHALL happen asynchronously after the result, with its own timeout and a logged outcome, so the part after the stop is local and bounded. Empty SHALL mean no commits ahead of the worker's base ref and a clean working tree, determined locally and failing closed. Otherwise it SHALL refuse with a reason naming the verdict and SHALL change nothing. With `force` true it SHALL skip the classification, but it SHALL still compute an unmerged summary BEFORE deleting, and the result SHALL report whether role-scoped local cleanup completed or is pending owner retirement, without waiting for any fleet-wide reclaim reconciliation, which SHALL run asynchronously and log its outcome. The summary SHALL list (branch, commits ahead with subjects, dirty file count, last known PR state; any field it cannot determine SHALL read `unknown (reason)` and SHALL NOT block the delete) and the tool result SHALL carry that summary. The resident's orientation SHALL require it to report every force delete, naming the worker and what was unmerged, in its own conversation. A classification failure SHALL be treated as not safe. Deleting SHALL NOT require a confirmation prompt.

#### Scenario: Merged work is deleted

- **WHEN** the worker's branch is confirmed merged and `force` is absent
- **THEN** the worker is stopped and its worktree, branch and task are removed

#### Scenario: Empty work is deleted

- **WHEN** the worker has no commits ahead and a clean tree
- **THEN** it is deleted without needing a merged PR

#### Scenario: Uncommitted work is refused even when the branch is safe

- **WHEN** the worker's branch classifies as safe (merged, or no commits ahead) but its worktree has uncommitted or untracked files, and `force` is absent
- **THEN** the call is refused naming the dirty file count and the worker, worktree and branch are untouched

#### Scenario: Busy host refuses before stopping

- **WHEN** waiting for another resident mutation or slow session queries leaves too little of the call's budget before the stop
- **THEN** the call is refused with "host busy; retry" and nothing is stopped or deleted

#### Scenario: Busy worker is refused

- **WHEN** the worker's session is busy and `force` is absent
- **THEN** the call is refused with "worker is busy; wait, or force" and nothing changes

#### Scenario: Work written during classification is kept

- **WHEN** an idle worker's tree is clean at the check but becomes dirty, or gains a commit, before its session is stopped, and `force` is absent
- **THEN** the call is refused after the stop, naming the dirty files, and the role, binding and worktree are intact with the session left stopped

#### Scenario: Unmerged work is refused

- **WHEN** the worker has unmerged commits and `force` is absent
- **THEN** the call is refused with the verdict reason and the worker, worktree and branch are untouched

#### Scenario: Force skips the check

- **WHEN** `force` is true and the worker has unmerged commits
- **THEN** the worker is deleted and the result reports the destroyed branch

#### Scenario: Force result carries the unmerged summary

- **WHEN** `force` is true and the worker has three unmerged commits and two dirty files
- **THEN** the tool result names the worker and branch, lists the three commit subjects and the dirty-file count, and the worker is deleted

#### Scenario: Summary failure does not block

- **WHEN** `force` is true and the commit count cannot be computed
- **THEN** the result shows `unknown (reason)` for it and the worker is still deleted

#### Scenario: Summary does not wait on fleet reconciliation

- **WHEN** `force` is true and the fleet-wide reclaim reconciliation is slow
- **THEN** the tool result with the summary returns as soon as the worker's own stop and reclaim finish, and the reconciliation completes in the background with its outcome logged

#### Scenario: No prompt

- **WHEN** the resident calls `hera_delete_worker`
- **THEN** no confirmation is requested

#### Scenario: Classifier failure fails closed

- **WHEN** the merge-safety lookup fails for a network reason and `force` is absent
- **THEN** the delete is refused as not safe

#### Scenario: A resident cleanup waits for the owner to retire

- **WHEN** the role is nuked and its session owner has not finished retirement
- **THEN** the result reports pending local cleanup. The daemon retries cleanup after retirement. A remote branch deletion uses the tip captured before the role nuke, and a different tip is preserved.
