## ADDED Requirements

### Requirement: Dead client connection does not trigger a daemon restart

When the TUI's health-check pings fail repeatedly, it SHALL first dial a fresh connection to the daemon socket and ping it. If a live daemon answers, the TUI SHALL adopt that connection, reset its failure streak, and SHALL NOT shut down or spawn any daemon.

#### Scenario: Live daemon behind a dead connection
- **WHEN** three consecutive pings over the TUI's connection fail but a fresh connect-and-ping to the socket succeeds
- **THEN** the TUI swaps to the fresh client, logs the reconnect, and starts no daemon

#### Scenario: No daemon answers
- **WHEN** a fresh connect-and-ping also fails
- **THEN** the TUI proceeds with the cooldown-gated restart

### Requirement: Restart shutdown reaches the live daemon

When restarting the daemon, if the Shutdown RPC over the TUI's own connection fails, the TUI SHALL retry Shutdown over a fresh connection before spawning a replacement.

#### Scenario: Own connection dead, daemon alive
- **WHEN** Shutdown over the TUI's connection errors and a live daemon answers on a fresh connection
- **THEN** Shutdown is delivered over the fresh connection

### Requirement: AutoStart returns only the daemon it started

`AutoStart` SHALL NOT return a client connected to a daemon that owned the socket before the spawn. It SHALL record the pre-spawn daemon's boot time, and accept only a live daemon whose boot time differs (or any live daemon if none answered before the spawn), waiting long enough for the successor to replace its predecessor.

#### Scenario: Old daemon still owns the socket at spawn
- **WHEN** a replacement is spawned while the old daemon still answers on the socket
- **THEN** AutoStart keeps waiting, ignores the old daemon, and returns a client to the replacement once it owns the socket

#### Scenario: Replacement never appears
- **WHEN** only the pre-spawn daemon answers until the timeout
- **THEN** AutoStart returns an error rather than a client to the old daemon
