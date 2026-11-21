## ADDED Requirements

### Requirement: System notices carry an argus sender label

A self-addressed hera message whose body starts with `[argus] ` is a daemon system notice. Every surface that renders a hera message's sender (`hera_inbox`, `hera_get_messages`, the TUI inbox viewer, and the REST message list) SHALL label it `argus`. Other self-addressed messages SHALL keep the `Aaron (board)` label. The daemon SHALL insert system notices through the same validation as ordinary hera messages (body and tldr caps, inbox-full cap, active-recipient check), except that a self-addressed sender is allowed and the body must start with `[argus] `.

#### Scenario: Argus notice sender label

- **WHEN** a coordinator reads a self-addressed message whose body starts with `[argus] `
- **THEN** the sender is shown as `argus`

#### Scenario: Board notices keep their label

- **WHEN** a coordinator reads a self-addressed board notice whose body does not start with `[argus] `
- **THEN** the sender is shown as `Aaron (board)`

#### Scenario: System notice insert enforces caps

- **WHEN** the daemon inserts a system notice for an archived role, with an empty tldr, or with a body lacking the `[argus] ` prefix
- **THEN** the insert is rejected and nothing is stored
