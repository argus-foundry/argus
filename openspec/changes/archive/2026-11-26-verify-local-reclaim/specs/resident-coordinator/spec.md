## ADDED Requirements

### Requirement: Verify local resident worker cleanup

Resident reclaim SHALL confirm that each unprotected local target is absent before it marks the local step complete or proceeds to remote branch deletion.

#### Scenario: Git refuses to delete a local branch
- **WHEN** a local branch remains after a delete attempt
- **THEN** cleanup remains pending and no remote deletion is attempted.

#### Scenario: A replacement claims the same branch and commit
- **WHEN** a surviving task now uses the branch as its branch or base
- **THEN** local and remote cleanup preserve the reference even if its commit still matches the saved lease.

#### Scenario: A stack push stalls
- **WHEN** another cleanup holds admission while deleting a stacked branch
- **THEN** Git network operations are bounded so task admission can resume.

#### Scenario: Stack branches share names across repositories
- **WHEN** another repository has a stack with the same branch names
- **THEN** its tasks cannot supply evidence or admission for deleting refs in the current repository.
