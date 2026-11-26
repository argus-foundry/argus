## ADDED Requirements

### Requirement: Verify actual repository and task ownership

Resident and stacked cleanup SHALL use the repository identified by a surviving task's existing linked worktree. Deferred cleanup SHALL preserve the captured Git common-directory and origin identities. Merge safety SHALL use the repository and default branch associated with the checkout. Stack discovery SHALL carry the captured identity and branch tip to deletion. Local and remote stack deletion SHALL compare against the classified tip. Local cleanup SHALL retain checked-out branches and branches reserved by an ongoing rebase or bisect. Its independently verified remote branch MAY still be deleted with a lease to the captured push destination. Repository and lease lookups SHALL finish within the pre-mutation budget and local settlement MAY defer if the overall deadline expires.

#### Scenario: A linked worktree survives a project remap
- **WHEN** another task still uses the branch in its existing linked worktree
- **THEN** cleanup preserves the branch even if that task's project now points elsewhere.

#### Scenario: An unrelated repository or origin takes the original name
- **WHEN** the Git common directory is replaced at its old pathname or its origin is changed after lease capture
- **THEN** the old cleanup does not mutate the replacement repository or origin.

#### Scenario: A remapped project's default does not apply to an older checkout
- **WHEN** the surviving linked checkout and the configured project resolve to different repositories
- **THEN** non-force resident deletion and stack classification refuse to use merge evidence from that project.

#### Scenario: An origin branch advances after stack classification
- **WHEN** a pushed branch has a different tip from the one classified for cleanup
- **THEN** the remote branch is preserved.

#### Scenario: Lease capture exhausts the mutation budget
- **WHEN** repository or ref checks consume the time left before the first destructive step
- **THEN** resident deletion leaves the session and binding unchanged.
