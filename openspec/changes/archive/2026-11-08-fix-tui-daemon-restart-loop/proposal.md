## Why

A daemon bounce by another process left the TUI restarting the daemon every ~30 s forever, disconnecting every agent from the MCP server each time. Two defects sustain the loop: the TUI treats "my connection died" as "the daemon died", and `AutoStart` returns a client connected to the OLD daemon (which still owns the socket and is killed ~1 s later by its successor).

## What Changes

- Before restarting on 3 failed pings, the TUI dials a fresh connection; if a live daemon answers it adopts that client and spawns nothing.
- `restartDaemon` retries the Shutdown RPC over a fresh connection when the TUI's own connection is dead.
- `AutoStart` records the pre-spawn daemon's `BootInfo.BootedAt` and only returns a client whose daemon has a different `BootedAt` (a true successor). No wire/protocol change.

## Capabilities

### Modified Capabilities
- `daemon-lifecycle`: client-side restart/auto-start must not adopt a doomed predecessor, and a dead client connection must not trigger a restart while a live daemon answers.

## Impact

`internal/daemon/client` (new `ConnectLive`, `awaitSuccessor`; `autostart_fork.go`), `internal/tui/app.go` health check and `restartDaemon`. Callers of `AutoStart` (`cmd/argus/main.go`, TUI restart/skew modal) all go through the fixed path; main.go only auto-starts after `Connect` failed. Frontend parity: TUI-only client behavior; web and macOS clients have no restart path. 
