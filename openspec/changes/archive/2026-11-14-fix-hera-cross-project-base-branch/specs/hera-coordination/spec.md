## ADDED Requirements

### Requirement: Automatic plan bases belong to the target project

The gater SHALL validate every nonempty derived plan-node base against the target project. Repository resolution SHALL perform a bounded best-effort fetch first; foreign source-task provenance MAY be rejected without fetching. If the branch does not resolve locally or through upstream/origin tracking refs, or its source task belongs to another project, it SHALL pass an empty base to task creation so the target project's configured default is used. This SHALL apply equally to workers and sub-coordinators and SHALL log the rejected base and target project.

#### Scenario: Cross-project root node

- **WHEN** a root node targets another project than its coordinator task
- **THEN** the target project's default branch is used even if the coordinator branch name exists there

#### Scenario: Cross-project blocker

- **WHEN** the selected blocker task belongs to another project
- **THEN** the target project's default branch is used

#### Scenario: Missing derived branch

- **WHEN** the orchestrator's explicit base cannot resolve in the target repository after best-effort fetch
- **THEN** the target project's default branch is used

#### Scenario: Same-project stacking

- **WHEN** the selected same-project blocker or coordinator branch resolves in the target repository
- **THEN** that derived branch remains the base

#### Scenario: Sub-coordinator and child roots

- **WHEN** a sub-coordinator node or a child orchestrator's root node derives an unsuitable base
- **THEN** it uses its own target project's default by the same validation rule

#### Scenario: Direct worker spawning

- **WHEN** hera_spawn_worker omits branch
- **THEN** task creation uses the selected project's configured default
- **AND** an explicitly supplied nonexistent branch still fails rather than silently falling back
