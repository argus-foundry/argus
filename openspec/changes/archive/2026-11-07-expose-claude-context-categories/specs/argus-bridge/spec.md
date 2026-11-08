## ADDED Requirements

### Requirement: Measurements carry optional context categories
`session.measure` context SHALL optionally carry non-negative integer token counts for `messages`, `memory`, `tools`, `agents`, `skills`, `system` and `compaction`, taken from the engine's context breakdown. A category the engine did not report SHALL be absent, never zero-filled. Breakdown reads SHALL run only for released context measurements, in background single-flight work with a short deadline. Timeout or read failure SHALL send the measurement without categories. A timed-out unresolved read SHALL retain the flight, and results from a replaced session SHALL be discarded. The daemon SHALL drop only a measurement with negative or nonfinite context, log it, ingest all other valid events in the batch, return success with accepted/received counts, and keep the previous context until a valid measurement replaces it. A new session id or launch generation SHALL clear stored categories.

#### Scenario: Missing versus zero
- **WHEN** a measurement reports `memory: 0` and omits `agents`
- **THEN** the stored snapshot holds memory 0 and agents unknown

#### Scenario: Invalid measurement
- **WHEN** a measurement carries a negative category
- **THEN** the response is 200, the invalid measurement is not accepted, and the previous context snapshot is unchanged

#### Scenario: Invalid measurement shares a batch with lifecycle events
- **WHEN** an invalid measurement precedes or follows valid needs_input, cleared, turn.start, turn.complete or heartbeat events
- **THEN** the valid events update the authoritative snapshot consumed by all clients, and the response is 200 with counts excluding only the invalid measurement
