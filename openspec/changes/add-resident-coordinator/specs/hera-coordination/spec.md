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

### Requirement: hera_delete_worker is a resident-only coordinator tool

Coordinator-class hera tools available to regular coordinators SHALL NOT include `hera_delete_worker`; it SHALL be reachable only by the resident under the exclusivity rules of the `resident-coordinator` capability. A code-level test SHALL assert that a regular coordinator's connection cannot list or call it.

#### Scenario: Regular coordinator cannot delete

- **WHEN** a regular coordinator calls `hera_delete_worker` for one of its own workers
- **THEN** the call fails with a resident-only error and the worker is unchanged
