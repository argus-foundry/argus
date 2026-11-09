## ADDED Requirements

### Requirement: Resume and restart honor Hera close-out

`POST /api/tasks/{id}/resume` and `/restart` SHALL accept an optional JSON body `{"force_revive":bool}` (empty body means false; malformed JSON, non-object JSON, bodies exceeding 1024 bytes, and trailing data are 400 `invalid_body`). For a task holding a live worker or freelance Hera binding that satisfies the shared close-out predicate (`ready_to_close` meta, or a terminal done/failed role status), an ordinary request SHALL return 409 `{"error":"task awaits Hera close-out","code":"hera_closeout","can_force_revive":true}` without spawning, reattaching or changing task status, markers or role status. Coordinator and unbound tasks SHALL keep normal behavior. The existing busy (`task_running`) and pending-restart (`agent_restarting`) 409s SHALL apply for every task status and take precedence over the close-out guard, and unknown tasks SHALL return 404 `task_not_found`. With `force_revive:true` the server SHALL snapshot and then clear close-out state (ClearHeraCloseout semantics) before launching, and SHALL restore the snapshot only on a definite launch failure (500 `start_failed`). Rollback SHALL preserve original row absence and timestamps and SHALL compare the current state against the cleared state in one transaction so newer writes survive. An ambiguous launch outcome (`agent.ErrStartAmbiguous`) SHALL return 503 `start_ambiguous` without restoring close-out markers; callers SHALL check task runtime before retrying. The server SHALL never retry with force on its own. Resume, restart and archive/input SHALL share one per-task lock. Request bodies SHALL be read and validated before acquiring this lock so stalled uploads cannot block other task operations. Status and session events keep the existing emission points: `session.started` only when a process actually starts, `task.status_changed` only on an actual status transition.

#### Scenario: Ordinary resume of a closed-out worker

- **WHEN** a worker with `ready_to_close=true` is resumed without force
- **THEN** the response is 409 `hera_closeout` and status, markers and sessions are untouched

#### Scenario: Forced revival definitely fails to launch

- **WHEN** `force_revive:true` is sent and the launch definitely fails
- **THEN** the response is 500 `start_failed` and the prior markers and role status are restored only where the cleared state is unchanged

#### Scenario: Forced revival has an ambiguous launch outcome

- **WHEN** `force_revive:true` is sent and the launch returns `agent.ErrStartAmbiguous`
- **THEN** the response is 503 `start_ambiguous` and close-out markers and role status are not rolled back

#### Scenario: Forced revival succeeds

- **WHEN** `force_revive:true` is sent and the session starts
- **THEN** markers are cleared, status is in_progress, and the response is `resumed` or `restarted` (`healed:true` on reattach)
