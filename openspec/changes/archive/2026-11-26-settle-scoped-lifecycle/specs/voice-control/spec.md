## ADDED Requirements

### Requirement: Scoped lifecycle recovery

Terminal reconnects SHALL retain the requested process identity through stream attachment. Native clients SHALL retry temporary snapshot conflicts during replacement.

#### Scenario: Process changes during attachment
- **WHEN** a terminal reconnect requests an offset for the old process and a replacement is installed before attachment
- **THEN** the request receives a stale-session conflict and cannot apply the old offset to the replacement.

#### Scenario: Previous supervisor cannot manage a live backend move
- **WHEN** a live Claude or Codex task is moved between backends and the session owner cannot manage its restart
- **THEN** the move fails before persisting its new backend or handoff.

#### Scenario: Hera stop delivery is interrupted
- **WHEN** the daemon finds a durable reclaim intent for an archived, unbound task whose exact process is still live
- **THEN** it retries the PID-scoped stop and waits for a matching exit before reclaiming.

#### Scenario: Hera role admission overlaps reclaim
- **WHEN** a task is being stopped or its worktree is being removed by Hera reclaim
- **THEN** a new live binding for that task cannot commit during reclaim.

#### Scenario: Daemon reattaches while a launch is reserved
- **WHEN** the owner has reserved a launch or a managed successor is still starting
- **THEN** owner listings retain the reservation and the new daemon keeps tracking the launch until the owner settles it.
