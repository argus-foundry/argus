## ADDED Requirements

### Requirement: Agent re-render action

The keymap SHALL define a rebindable `CtxAgent` action `agent.rerender`, default `shift+home`, labelled "re-render at this width". The default SHALL NOT collide with any existing default binding, and SHALL NOT shadow a key the Claude Code composer consumes (ctrl+a/e/u/w/k/o/b and the other readline and Claude chords remain forwarded to the PTY). The action SHALL appear in the generated help overlay, the README Reference keybinding table, and the command palette for agent-view and Hera pane focus, each showing the live resolved key. An override that violates the agent-context modifier rule SHALL be rejected with the default kept.

#### Scenario: Default binding resolves

- **WHEN** no override is configured and the user presses shift+home in a focused agent pane
- **THEN** `agent.rerender` is dispatched and the key is not forwarded to the PTY

#### Scenario: Composer chords still reach the PTY

- **WHEN** the user presses ctrl+e, ctrl+a, ctrl+u, ctrl+w, ctrl+k or ctrl+o in a focused agent pane
- **THEN** the key is forwarded to the PTY as before

#### Scenario: Rebind shows in help and notice

- **WHEN** `agent.rerender` is rebound to another valid chord
- **THEN** the help overlay, palette row and owed notice show the new chord

#### Scenario: Plain key rejected

- **WHEN** config.toml binds `agent.rerender` to a plain letter
- **THEN** the override is rejected with a warning and the default stays
