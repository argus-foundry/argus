## ADDED Requirements

### Requirement: One recycle runner for the resident

For a task where `IsResident()`, every recycle trigger SHALL execute through the resident recycle runner: the agent's `task_recycle` call, a Hera `request_recycle` flag acted on by the recycle watcher, a human-forced coordinator recycle and the TUI bounce action. The Hera coordinator runner's restart step SHALL delegate to the same restart path (stray-job cleanup, resident PTY role, resident seed), and the in-flight guard SHALL be shared so two triggers cannot overlap or double-restart. The resident seed SHALL keep its ordering (handoff note, open-loops-first) and SHALL gain a team section listing each worker's role name, project, status, last self-reported status line, unread messages and whether it is flagged blocked. Recycling SHALL NOT end the resident's binding or touch any worker.

#### Scenario: Two triggers, one recycle

- **WHEN** the resident calls `task_recycle` and the watcher also fires for the same resident
- **THEN** exactly one restart occurs and the second trigger is rejected or no-ops

#### Scenario: Hera trigger uses the resident seed

- **WHEN** a human forces a coordinator recycle on the resident
- **THEN** the fresh session is seeded with the resident seed including the team section, not the coordinator plan-state seed

#### Scenario: Team section content

- **WHEN** the resident has a worker flagged blocked and recycles
- **THEN** the seed names that worker and marks it blocked

#### Scenario: Workers survive

- **WHEN** a recycle completes
- **THEN** every worker session and binding is unchanged
