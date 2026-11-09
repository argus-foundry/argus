## ADDED Requirements

### Requirement: Hera roster context measurements

Every role in `GET /api/hera` SHALL carry `context`, null when the bound task has no valid `context_size` observation. Otherwise it SHALL carry `tokens`, `denominator_tokens`, `denominator_kind` (`worker_window` for worker/freelance, `coordinator_recycle_budget` for coordinators), `percent` clamped to 0-100, and `observed_at`. `observed_at` SHALL stay null unless the producer supplies a verified timestamp. A malformed observation SHALL yield `context: null` for that role only. All measurements SHALL arrive in the single roster response, with no per-role transcript reads.

#### Scenario: Worker versus coordinator denominator
- **WHEN** a worker and a coordinator each report 120000 tokens
- **THEN** the worker's percent uses `worker_context_window` and the coordinator's uses `coordinator_context_budget`

#### Scenario: Unavailable is not zero
- **WHEN** a role has no, or unparseable, `context_size`
- **THEN** `context` is null

#### Scenario: Missing budget
- **WHEN** the applicable budget is not positive
- **THEN** `tokens` is retained and `denominator_tokens` and `percent` are null
