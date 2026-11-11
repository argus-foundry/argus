## ADDED Requirements

### Requirement: Tasks-tab pane re-resolves a dead handle and Enter never restarts a live agent

The Tasks-tab agent pane SHALL, on each tick, replace a dead session handle with a live, different handle when the daemon still reports the session alive. Enter on a pane without a live handle SHALL first re-check liveness: if the daemon reports the session alive it SHALL attach the fresh handle and SHALL NOT call startSession or change the task status; only an absent session proceeds to startSession.

#### Scenario: Stream lost, agent alive

- **WHEN** the pane holds a dead handle and the daemon reports the agent alive
- **THEN** the next tick (or Enter) attaches a fresh handle, the task status is unchanged, and no "session already exists" error occurs

#### Scenario: Agent really gone

- **WHEN** the daemon reports no session and the user presses Enter
- **THEN** startSession runs as before
