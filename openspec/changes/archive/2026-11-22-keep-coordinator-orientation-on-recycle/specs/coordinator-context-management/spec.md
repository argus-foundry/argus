## ADDED Requirements

### Requirement: A recycled coordinator's seed carries its standing orientation

The hera recycle seed prompt for a coordinator-kind role SHALL include a "Standing coordinator orientation (still in force)" section containing the coordinator orientation for that role. The orientation SHALL be derived at recycle time from the role's current live structure and SHALL NOT be read from the task's prompt (which each recycle overwrites), so it is present after any number of consecutive recycles. The section SHALL state that the rules still apply but are not a new task, and SHALL appear after the relay directive and before the "Original prompt (background only …)" section.

The orientation SHALL be chosen as follows:

- When the coordinator's task holds a live binding to a worker-kind role in a different orchestrator (a sub-coordinator), the section SHALL contain `HeraSubCoordinatorOrientation` naming the coordinator's own orchestrator, that parent orchestrator, and the coordinator role. When that parent worker role is a `kind=subcoord` plan node, the section SHALL ALSO contain `HeraCheckInOrientation` for the parent orchestrator and its coordinator, preceded by a line stating that the prior session already completed the launch check-in and that the fresh session must not check in again or wait for a new "go" unless its handoff note says it was still waiting.
- Otherwise (a top-level coordinator), the section SHALL contain `HeraCoordinatorOrientation` for the coordinator's orchestrator.

Worker-kind and freelance-kind recycle seeds SHALL NOT contain this section. A failure to resolve the orchestrator SHALL omit the section and SHALL NOT block the recycle. A failure to list the task's bindings SHALL fall back to the top-level orientation. Either failure SHALL be logged.

#### Scenario: Top-level coordinator recycle carries the coordinator orientation

- **WHEN** a top-level coordinator of orchestrator X is recycled
- **THEN** the seed prompt contains the "Standing coordinator orientation (still in force)" section with `HeraCoordinatorOrientation("X")`, placed before the original-prompt section

#### Scenario: The orientation survives two consecutive recycles

- **WHEN** a top-level coordinator is recycled, and then recycled again with no other change
- **THEN** the second seed prompt still contains the full coordinator orientation, even though the task prompt was replaced by the first seed

#### Scenario: A sub-coordinator plan node keeps its sub-coordinator orientation and check-in

- **WHEN** the coordinator of a child orchestrator materialized from a `kind=subcoord` plan node in parent orchestrator P is recycled
- **THEN** the seed prompt contains `HeraSubCoordinatorOrientation` naming the child orchestrator and P, `HeraCheckInOrientation` for P, and the line saying the launch check-in was already completed, and does not contain the top-level coordinator orientation

#### Scenario: A self-promoted worker gets the sub-coordinator orientation without check-in

- **WHEN** a coordinator whose task also holds a live plain worker-kind binding in orchestrator P is recycled
- **THEN** the seed prompt contains `HeraSubCoordinatorOrientation` naming P and no check-in text

#### Scenario: Worker recycles carry no coordinator orientation

- **WHEN** a worker-kind or freelance-kind role is recycled
- **THEN** its seed prompt contains no standing-orientation section and no coordinator orientation text

#### Scenario: Orientation lookup failure never blocks the recycle

- **WHEN** the coordinator's orchestrator cannot be resolved while building its seed
- **THEN** the seed is built without the orientation section, a `[recycle]` log line is written, and the recycle proceeds

### Requirement: Hera recycle seeds are marked as Argus-injected

The hera recycle seed prompt SHALL begin with the `[argus] ` system-turn prefix (`lastexchange.SystemTurnPrefix`) for every role kind, so that when a recycled session is recycled again before any human message, the last-exchange extraction does not treat the previous seed as the user's last message.

#### Scenario: A second recycle with no human turn does not quote the previous seed

- **WHEN** a session whose only user turn is a hera recycle seed is recycled again
- **THEN** the extracted last exchange contains no human message from that seed, and the new seed has no "Last exchange with the user" section built from it

#### Scenario: A human message after the seed is still extracted

- **WHEN** a human replies after a hera recycle seed and the session is recycled again
- **THEN** the human's reply is extracted as the user's last message
