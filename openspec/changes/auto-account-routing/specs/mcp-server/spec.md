## MODIFIED Requirements

### Requirement: task_create account selection and inheritance

`task_create` SHALL accept optional `account`, `caller_id` and `cwd` parameters. An explicit `account` (`"default"` included) SHALL be used as given, except that `"auto"` SHALL be treated as no account; when the task store exposes configuration, an unknown account SHALL return a tool error. Without `account`, a caller identified by `caller_id` or `cwd` SHALL pass its own stored account to the new task as an inherited account only when that account is non-blank and the caller is not routed; a blank-account or routed caller SHALL pass no account so the new task is routed. An inherited account that cannot run the backend SHALL fail the call. When the caller cannot be resolved, the account SHALL resolve through the project/global default and the miss SHALL be logged.

#### Scenario: Worker spawned by an agent inherits its account
- **WHEN** a task pinned to `personal` calls `task_create` with its `caller_id` and no `account`
- **THEN** the new task runs on `personal`

#### Scenario: Unknown explicit account
- **WHEN** `task_create` is called with `account: "nope"`
- **THEN** the response is a tool error and no task is created

#### Scenario: Routed caller lets the child route
- **WHEN** a routed task on `personal` calls `task_create` with its `caller_id` and no `account`
- **THEN** the new task's account comes from routing and it is marked routed

#### Scenario: Blank-account caller lets the child route
- **WHEN** a task with a blank stored account calls `task_create` with its `caller_id` and no `account`
- **THEN** the child is routed, not pinned to `default`

#### Scenario: auto alias
- **WHEN** `task_create` is called with `account: "auto"`
- **THEN** it behaves as if no account were given
