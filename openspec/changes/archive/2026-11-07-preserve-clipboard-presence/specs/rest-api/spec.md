## ADDED Requirements

### Requirement: Stream clipboard events preserve presence

On `/api/tasks/{id}/stream`, the `clipboard` event SHALL be `{"text":""}` for a staged empty value, `{"text":"…"}` for text, and `{"cleared":true}` only for clear or expiry. The connect snapshot and live events SHALL use the same semantics.

#### Scenario: Snapshot and live agree

- **WHEN** an empty value is staged before and after the stream connects
- **THEN** both events are `{"text":""}`
