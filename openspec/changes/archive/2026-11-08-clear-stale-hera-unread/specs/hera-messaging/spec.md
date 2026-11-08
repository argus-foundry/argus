## MODIFIED Requirements

### Requirement: Reliable doorbell delivery via the shared notifier

The system SHALL deliver a message to the recipient's live argus task by enqueueing a doorbell line through the existing `notify.Notifier` (`ReliableNotify`) — the single idle-gated, exactly-once pane-delivery implementation, the same one `task_messages` uses. The doorbell line carries the from-role name, message id, tldr, and a fetch hint telling the recipient to read the body with `hera_inbox`; it SHALL keep the `[hera from ` prefix. Delivery uses a per-message delivery id prefixed `hera:` so it never collides with `task_messages` delivery ids. Delivery is best-effort and never rolls back the stored message.

Derived from: `internal/hera/service.go:67` (`Send`), `internal/hera/service.go:105` (doorbell line), `internal/hera/service.go:186` (`heraDeliveryID` "hera:" prefix), `internal/hera/service.go:31` (`Notifier` = `notify.Notifier`).

#### Scenario: Live recipient gets a doorbell

- **WHEN** the recipient role holds a live binding
- **THEN** ReliableNotify enqueues a `[hera from <role>] msg #<id> — <tldr> · read: hera_inbox` doorbell to that task and the message is stamped idle_submit

#### Scenario: Delivery failure does not roll back storage

- **WHEN** the notifier fails or is nil
- **THEN** the message remains durably stored and the send still succeeds

## ADDED Requirements

### Requirement: Mail for permanently gone roles is cleared

Unread messages addressed to a role SHALL be marked read when that role's last live binding ends for a terminal reason (`user_deleted`, `argus_deleted`, `task_missing`, or `moved`), so mail for a role that can never read it does not accumulate. A binding that ends for a non-terminal reason (for example `hera_rebind`, `reparented`, or a materialize failure) SHALL leave the role's mail unread, because a role-addressed message waits for a later binding. A role that holds a live binding SHALL keep its mail. At daemon startup the system SHALL idempotently apply the same rule to existing data: unread mail for any role with no live binding that is nuked, or whose most recent binding ended terminally, is marked read; roles that were never bound are untouched.

#### Scenario: Terminal binding end clears unread mail

- **WHEN** a worker's only binding ends with reason `user_deleted`
- **THEN** every unread message addressed to that role is marked read

#### Scenario: Non-terminal end keeps mail

- **WHEN** a binding ends with reason `hera_rebind`
- **THEN** unread messages addressed to the role stay unread

#### Scenario: Role with a live binding keeps mail

- **WHEN** the startup sweep runs while a role holds a live binding
- **THEN** its unread mail stays unread

#### Scenario: Startup sweep is idempotent and spares unbound roles

- **WHEN** the daemon starts with unread mail on a role whose last binding ended `user_deleted` and on a role that was never bound
- **THEN** only the first role's mail is marked read, and a second sweep changes nothing
