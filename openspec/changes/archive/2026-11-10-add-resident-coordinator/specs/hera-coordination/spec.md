## ADDED Requirements

### Requirement: hera_new_orchestrator interplay with the resident

`hera_new_orchestrator` SHALL reject any caller that `IsResident()` before the existing coordinating-another-orchestrator scan, failing closed when the caller cannot be classified, with a message directing it to `hera_spawn_worker`. It SHALL reject any request for the reserved name `resident` from every other caller. The daemon's creation of the standing orchestrator SHALL NOT pass through this handler, so the existing guard SHALL NOT block it. The existing guard for non-resident callers SHALL be unchanged.

#### Scenario: Resident calls it

- **WHEN** the resident calls `hera_new_orchestrator` with any name
- **THEN** the call is rejected with guidance to spawn workers and nothing is created

#### Scenario: Reserved name for others

- **WHEN** a regular worker calls `hera_new_orchestrator(name="resident")`
- **THEN** the call is rejected as a reserved name

#### Scenario: Guard unchanged for others

- **WHEN** a regular coordinator calls it for a different name
- **THEN** the existing "already coordinating" rejection still applies

### Requirement: hera_delete_worker and hera_graduate_worker are resident-only coordinator tools

Coordinator-class hera tools available to regular coordinators SHALL NOT include `hera_delete_worker`; it SHALL be reachable only by the resident under the exclusivity rules of the `resident-coordinator` capability. A code-level test SHALL assert that a regular coordinator's connection cannot list or call it. The same SHALL hold for `hera_graduate_worker`.

#### Scenario: Regular coordinator cannot delete

- **WHEN** a regular coordinator calls `hera_delete_worker` for one of its own workers
- **THEN** the call fails with a resident-only error and the worker is unchanged

#### Scenario: Regular coordinator cannot graduate

- **WHEN** a regular coordinator calls `hera_graduate_worker` for one of its own workers
- **THEN** the call fails with a resident-only error and the worker is unchanged

### Requirement: hera_new_orchestrator rejects resident workers

`hera_new_orchestrator` SHALL reject a caller that holds a live worker binding in the resident's standing orchestrator, fail-closed, before the existing coordinating-another-orchestrator scan, with a message directing it to ask the resident to graduate it. The existing behavior for every other worker SHALL be unchanged.

#### Scenario: Resident worker is rejected

- **WHEN** a resident worker calls `hera_new_orchestrator`
- **THEN** the call is rejected with the graduation hint and nothing is created

#### Scenario: Ordinary worker unchanged

- **WHEN** a worker of a regular coordinator calls it
- **THEN** the existing worker self-promotion behavior applies

### Requirement: Graduating ends a worker binding with its own reason

A worker binding ended by graduation SHALL carry end reason `graduated`, distinct from `moved`, and mail addressed to the ended worker role SHALL be cleared as for a move.

#### Scenario: End reason recorded

- **WHEN** a worker is graduated
- **THEN** its ended binding has end reason `graduated`

