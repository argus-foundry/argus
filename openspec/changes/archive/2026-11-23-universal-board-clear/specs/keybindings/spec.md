## MODIFIED Requirements

### Requirement: Context-scoped config overrides

The system SHALL read user overrides from `[keybindings.<context>]` tables in config.toml, where each entry maps an action id (the suffix after the context) to a keyspec string. Overrides SHALL layer on top of the built-in defaults; absent entries keep their default. Unknown action ids and unknown tables SHALL be ignored without error.

#### Scenario: Rebind an action

- **WHEN** config.toml contains `[keybindings.tasklist]` with `new = "x"`
- **THEN** pressing `x` in the task list opens the new-task form and pressing `n` does nothing

#### Scenario: Live reload

- **WHEN** the user edits a `[keybindings.<context>]` entry while argus is running
- **THEN** the new binding takes effect without a restart (the keymap rebuilds when the config changes)

#### Scenario: Retired board done override

- **WHEN** config.toml retains hera_board.done with any keyspec, including an invalid one
- **THEN** loading and keymap building succeed without warnings, the retired override is ignored and default x still resolves to hera_board.dismiss

### Requirement: Help overlay reflects active bindings

The system SHALL generate the help overlay's argus sections from the keymap so the displayed keys match the resolved bindings, including user overrides.

#### Scenario: Help shows the override

- **WHEN** the task-list `new` action is rebound to `x`
- **THEN** the help overlay's Task List section shows `x` for "new task"

#### Scenario: Universal board clear help

- **WHEN** generated help displays board actions
- **THEN** it describes clear as dismissing FYIs, completing actions/todos and denying calls using the active dismiss binding, without a separate done row
