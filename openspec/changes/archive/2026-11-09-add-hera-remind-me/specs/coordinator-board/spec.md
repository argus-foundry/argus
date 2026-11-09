## ADDED Requirements

### Requirement: Human-directed board capture

The system SHALL ship a builtin `hera-remind-me` skill that Aaron invokes to have the current agent put an item from the conversation on its coordinator's board. With argument text the agent SHALL capture what the text names; without it the agent SHALL capture the most recent actionable follow-up from the conversation. The item SHALL have a one-line subject and a body that states the action, its trigger condition, its rationale and any concrete references. Its kind SHALL be `fyi` unless Aaron must make a choice, in which case it SHALL be `decision`.

A coordinator SHALL list its open items first and update an existing item that already covers the same follow-up instead of posting a duplicate. A worker or freelance agent SHALL NOT call board tools; it SHALL relay the item to its coordinator with `hera_send` and ask the coordinator to post it. An agent with no Hera binding SHALL report that no board exists in this session and SHALL NOT post or send anything. The skill SHALL NOT change board ownership or authorization.

#### Scenario: Coordinator captures a release follow-up

- **WHEN** a coordinator has just said "when we release, bump the service worker version" and Aaron runs `/hera-remind-me`
- **THEN** a new open `fyi` item exists on that coordinator's board whose subject names the follow-up and whose body includes the release trigger and the action

#### Scenario: Existing item covers the follow-up

- **WHEN** the coordinator's board already has an open item for the same follow-up and Aaron runs `/hera-remind-me` for it
- **THEN** the existing item is updated with the newer information and no second item is posted

#### Scenario: Worker relays to its coordinator

- **WHEN** Aaron runs `/hera-remind-me` in a worker session
- **THEN** the worker sends its coordinator a Hera message containing the proposed subject, kind and body with a request to post it, and makes no board tool call

#### Scenario: Plain task has no board

- **WHEN** Aaron runs `/hera-remind-me` in a task with no Hera binding
- **THEN** the agent says there is no board for this session and nothing is posted or sent
