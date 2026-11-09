## Why

A recycled session has no session ID. `restartSessionWithSeedPrompt` clears `task.SessionID` ("so BuildCmd mints a fresh one"), but BuildCmd never mints one; only `agent.CreateAndStart` does. A recycled Claude session therefore runs with an empty ID until it exits and post-exit capture recovers it, which hera workers often never do. ctrl+n re-render then refuses with "This backend can't re-render (no resumable session)". Verified live on a recycled coordinator task.

## What Changes

- A recycle mints a fresh pinned session ID (via a shared `agent.FreshSessionID`, also used by `CreateAndStart`) for families that pin `--session-id`; capture-style families still clear to empty.
- The fresh ID rides the supervisor `Recycle` RPC (`RecycleReq.SessionID`, additive) so the supervisor's BuildCmd actually passes `--session-id <new uuid>`. Without this the daemon-side persist alone does not reach the spawn.
- `ProtocolVersion` 11 to 12; `SupervisorSpawnSurface` 38 to 39.

## Capabilities

### Modified Capabilities

- `task-recycle`: restart mints a fresh pinned `SessionID` instead of clearing it.

## Non-Goals

- Recovering the ID of an already-running recycled session. It gets an ID at its next exit capture or its next recycle.
- Other paths that clear `SessionID`: audited, no other gap (the TUI start-failure clear only undoes an ID it just generated).

## Impact

`internal/agent/create.go`, `internal/daemon/{recycle.go,types.go,sessioncore.go,surface.go}`, `internal/daemon/client/client.go`. A stale supervisor ignores the new field and behaves as before; deploy needs a supervisor restart for the fix to take effect on supervisor-spawned sessions.
