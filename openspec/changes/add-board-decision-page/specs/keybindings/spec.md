## ADDED Requirements

### Requirement: Rebindable board decide action

The keymap SHALL define a rebindable action `hera_board.decide` in the `hera_board` context with default binding `o` and the help label "answer decisions in browser". The action SHALL appear in the generated `?` help overlay and the README keybinding table, SHALL be resolved through `keymap.Resolve` at the board key handler, and SHALL NOT collide with another binding in the `hera_board` or global contexts at default settings.

#### Scenario: Default binding

- **WHEN** no keymap override exists and the board has focus
- **THEN** `o` resolves to `hera_board.decide`

#### Scenario: Rebound

- **WHEN** the user rebinds `hera_board.decide` to another key
- **THEN** that key opens the decision page, `o` no longer does, and the help overlay and board hint show the new key

#### Scenario: Help listing

- **WHEN** the help overlay is opened on the Hera tab
- **THEN** the board section lists the decide action with its label and current binding
