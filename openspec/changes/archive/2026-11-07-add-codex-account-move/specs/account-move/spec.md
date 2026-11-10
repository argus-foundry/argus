## MODIFIED Requirements

### Requirement: A task can be moved to another account

The system SHALL provide an account-move operation for a task whose backend family declares a conversation transfer (Claude and Codex). The target SHALL be a configured account, `default`, or `auto`.

The move SHALL be rejected without side effects when the backend family has no transfer, the target is unknown, equals the current account, or does not support the backend, the target is `auto` with no candidate, the session is live and not idle without force, another move or restart of the task is in progress, or the target is not signed in or has no usable session storage (Codex: not signed in, or no `state_5.sqlite`).

For a Codex task the system SHALL copy the task's thread row (identity and folder columns excluded) into the target account's `state_5.sqlite` and its rollout file under the target home's `sessions/`, leaving the source and all credentials untouched, before storing the target account and restarting under it with `codex resume`. A missing source thread or rollout SHALL complete the move without resuming.

#### Scenario: Codex task moved and resumed
- **WHEN** a Codex task on `default` is moved to a signed-in Codex account
- **THEN** the thread and rollout exist under the target home, the source copy remains, no credentials are copied, and the session resumes the same conversation

#### Scenario: Codex target not ready
- **WHEN** the target Codex account is not signed in or has no state database
- **THEN** the move is rejected and the task's account and session ID are unchanged

#### Scenario: Family without a transfer refused
- **WHEN** a task on a backend family with no transfer is moved
- **THEN** the move is rejected naming the backend
