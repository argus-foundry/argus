## MODIFIED Requirements

### Requirement: Board `o` opens the decision page in the browser

With a visible coordinator board focused, the bound hera_board.decide key (default o) SHALL request a launch ticket and open the common rail/drawer decision page with that coordinator preselected, passing any selected open decision or FYI as focus. The same page SHALL permit selecting other rail-visible coordinators; launch role SHALL not restrict session scope. The key SHALL work without selection and on an empty or FYI-only board. An empty selected board SHALL show `Nothing open for <name>` rather than suppressing browser launch. TUI selection, expansion, read state, focus and composer SHALL remain unchanged. If REST is disabled the footer SHALL instruct enabling it in Settings and open nothing. In remote mode, where Hera is inert, the key SHALL do nothing. Launch SHALL retain the 90-second single-use fragment-ticket and eight-hour session lifetime and log success/failure/skips under board without credentials. The default key and rebindable action SHALL remain unchanged.

#### Scenario: Open current board

- **WHEN** Aaron presses o on a visible coordinator
- **THEN** the shared page opens with that coordinator selected and other visible coordinators available in the nav

#### Scenario: FYI focus

- **WHEN** Aaron presses o on an FYI-only board’s selected FYI
- **THEN** that coordinator and FYIs tab are selected and the FYI is focused

#### Scenario: Empty board

- **WHEN** Aaron presses o on a visible coordinator with no open items
- **THEN** the browser opens with that greyed coordinator selected and its empty state shown in both tabs

#### Scenario: No selection

- **WHEN** no board row is selected and Aaron presses o
- **THEN** the coordinator is preselected without an item focus target

#### Scenario: API disabled

- **WHEN** o is pressed with REST disabled
- **THEN** the footer says to enable the API in Settings and no browser opens

#### Scenario: Reply key unchanged

- **WHEN** r is pressed on any board item
- **THEN** the plain reply prefix is composed exactly as before
