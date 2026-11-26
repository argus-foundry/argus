## ADDED Requirements

### Requirement: Committed reclaim evidence and terminal teardown

Reclaim SHALL distinguish a committed cleanup from a role nuke that preserved a shared task. A deferred operation SHALL verify its binding before changing a task. Terminal view actions SHALL use the initiating view through completion.

#### Scenario: A preserved binding changes later
- **WHEN** a role is nuked while its task remains live-bound elsewhere
- **THEN** a later ordinary rebind or detach does not turn that older nuke into a reclaim request.

#### Scenario: A pending cleanup has already settled
- **WHEN** a delayed TUI reclaim callback picks up after the binding was settled
- **THEN** it leaves that retained task's current session and archive state alone.

#### Scenario: Generic mutation meets active reclaim
- **WHEN** generic archive, relaunch, or deletion meets a reclaim-owned task
- **THEN** it cannot change that task. Clear archive reports any role it could not nuke.

#### Scenario: Terminal session loses its view
- **WHEN** the user logs out or a resume request outlives its initiating detail view
- **THEN** no obsolete renderer or stream attaches.
