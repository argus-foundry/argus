## ADDED Requirements

### Requirement: Daemon runtime identity and restart-only control

`GET /api/runtime` SHALL serve to any authenticated token `{boot_id, daemon{hash,booted_at,state}, installed{hash}, supervisor{hash,state,compatibility}, restart_supported}`. `boot_id` is opaque and unique per daemon process. `state` is `current`, `stale` or `unknown` and compares content hashes against the installed executable; supervisor `compatibility` (`compatible`, `incompatible`, `unknown`) comes from the executed-surface verdict and is independent of hash equality. A missing installed executable, an unreachable supervisor or a failed BootInfo yields a null hash and `unknown`, never stale. Clients cannot supply executable paths.

`POST /api/daemon/restart` with `{"expected_boot_id": "..."}` SHALL require the master token and replace only the daemon with the already-installed binary: no build, no file replacement, no supervisor restart. It SHALL return 400 for a malformed or missing body, 403 for a non-master token, 503 when `restart_supported` is false (supervisor mode off, since agents would not survive), 409 when `expected_boot_id` differs from the current boot or a restart is already pending, 500 if acceptance fails, and otherwise 202 `{"restart_requested":true,"boot_id":"<current>"}` flushed before the successor is launched. The body SHALL contain exactly one JSON value followed only by whitespace. A failed launch or a successor exit before takeover SHALL clear the pending latch so a retry can proceed; while the successor is starting, the latch SHALL remain held. Completion is observed by reconnecting and reading a different `boot_id`; a retry against the successor conflicts.

#### Scenario: Concurrent requests
- **WHEN** several restart requests with the current boot id arrive together
- **THEN** exactly one is accepted and the successor is launched once

#### Scenario: Legacy in-process mode
- **WHEN** the daemon owns the agent PTYs itself
- **THEN** restart returns 503 and no process is signalled

#### Scenario: Stale boot id
- **WHEN** the client sends a boot id from before a restart
- **THEN** the response is 409 and nothing restarts

#### Scenario: Successor exits before takeover
- **WHEN** the accepted successor execs successfully but exits before replacing the serving daemon
- **THEN** the original daemon permits a later restart request instead of returning restart_pending forever
