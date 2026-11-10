## ADDED Requirements

### Requirement: Resident toggle and tab actions are global keymap actions

The keymap SHALL define `global.resident` (default `ctrl+\`) and `global.tab_resident` (default `0`) in `CtxGlobal` through `defaultSpecs`, `actionLabels` and `contextOrder`, so the `?` help overlay and the command palette list them automatically. `global.resident` SHALL be an unconditional global dispatch like `global.palette` and `global.switcher`. `keymap.Parse` SHALL accept the `ctrl+\` keyspec and map it to `tcell.KeyCtrlBackslash` (control byte 0x1c), pinned by a test in the same way as `ctrlKeys`. The README SHALL document the terminal-mapping recipe that sends 0x1c for Cmd+` in iTerm2 and Ghostty and note that macOS must not reserve Cmd+` for window cycling.

#### Scenario: ctrl+backslash parses

- **WHEN** `keymap.Parse("ctrl+\\")` is called
- **THEN** it returns a binding that matches `tcell.KeyCtrlBackslash` and round-trips through `String()`

#### Scenario: Help lists the actions

- **WHEN** the `?` overlay is generated
- **THEN** it lists the resident toggle and resident tab rows from the keymap with no static additions

#### Scenario: Unconditional dispatch

- **WHEN** the hotkey is pressed in the Tasks list, an agent view, the Hera tab or Settings with no modal open
- **THEN** it reaches the global handler and is not forwarded to any PTY

#### Scenario: Conflict-free default

- **WHEN** the default keymap is validated
- **THEN** `ctrl+\` and `0` conflict with no other global binding
