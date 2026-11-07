## MODIFIED Requirements

### Requirement: task.pr_changed event

The system SHALL publish `task.pr_changed` with the task id and payload `{"url","state","lifecycle","source"}` after a changed PR cache commit and after a manual link is set or cleared. `source` SHALL be `manual` or `automatic`. Setting a link SHALL publish `state` and `lifecycle` `unknown`. Consumers SHALL ignore payload keys they do not know.

#### Scenario: Payload shape

- **WHEN** a merged PR is first cached by the branch poller
- **THEN** the payload is `{"url":"...","state":"merged-closed","lifecycle":"merged","source":"automatic"}`

#### Scenario: Link set

- **WHEN** a manual PR link is set
- **THEN** the payload is `{"url":"<canonical>","state":"unknown","lifecycle":"unknown","source":"manual"}`
