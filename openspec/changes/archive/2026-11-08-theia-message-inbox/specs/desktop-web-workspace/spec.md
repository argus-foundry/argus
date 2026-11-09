## ADDED Requirements

### Requirement: Add read-only Agent messages and a separate coordinator human Inbox.

Agent messages use only GET hera-messages, oldest first, source plus ID identity, bounded older paging and retained scroll. Inbox is available only for coordinator task bindings. Unread mail shows one rail glyph without a count and opens Inbox. Expand/read, mark unread and dismiss send viewed expected_updated_at and apply authoritative responses. Revision conflicts refetch without acknowledging a newer revision. Reply appends item identity and subject to the captured composer draft without sending. Board events and reconnect reconcile board and roster. Task switches cancel and fence old responses.

#### Scenario: Obsolete responses

- **WHEN** a task or connection changes while a request is pending
- **THEN** late responses cannot overwrite the current view or draft

#### Scenario: Unavailable API

- **WHEN** a read or mutation fails
- **THEN** the view shows actionable retry feedback without claiming success
