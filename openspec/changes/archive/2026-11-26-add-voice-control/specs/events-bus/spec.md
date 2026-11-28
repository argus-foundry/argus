## MODIFIED Requirements

### Requirement: Global emission entry point

The system SHALL expose a single `Emit(type, taskID, payload)` entry point that constructs an event and forwards it to the installed sink, then to any subscribed listeners. When no sink is installed and no listeners are subscribed, emission SHALL be a silent no-op that never panics, so every call site can fire unconditionally. Listeners SHALL run on the emitter's goroutine after the sink returns, and a listener panic SHALL be recovered so later listeners and the caller still proceed.

#### Scenario: Emit with no sink installed

- **WHEN** `Emit` is called while no sink is registered and no listeners are subscribed
- **THEN** the call returns without error, delivers nothing, and does not panic

#### Scenario: Emit delivers a stamped event to the installed sink

- **WHEN** `Emit` is called with a type, task ID, and payload while a sink is installed
- **THEN** the sink receives exactly one event whose type and task ID match the inputs and whose timestamp is non-zero

#### Scenario: A subscribed listener receives the event without a sink

- **WHEN** `Emit` is called while a listener is subscribed and no sink is installed
- **THEN** the listener receives exactly one event whose type and task ID match the inputs

#### Scenario: A panicking listener does not stop the sink

- **WHEN** a listener panics while handling an event and a sink is installed
- **THEN** the sink still receives the event and a later listener still receives it
