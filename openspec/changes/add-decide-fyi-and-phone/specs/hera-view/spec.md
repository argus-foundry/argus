## MODIFIED Requirements

### Requirement: Board `o` opens the decision page in the browser

With the coordinator board focused, the bound `hera_board.decide` key (default `o`) SHALL request a launch ticket for that coordinator role and open the daemon-served decision page in the default browser, passing the selected open decision or FYI as focus. It SHALL work on any selected row or without selection whenever either kind has open items. It SHALL NOT change selection, expansion, read state, focus or the coordinator composer. Only when neither kind has open items SHALL it show a one-line no-open-items notice and open nothing. If the REST API is disabled it SHALL instruct the user to enable it in Settings and open nothing. In `--remote` mode, where the Hera page is inert, the key SHALL do nothing. Launches SHALL retain the existing single-role, 90-second fragment ticket and eight-hour session boundary and SHALL log success/failure/skipped outcomes under `[board]` without credentials. The default key and its rebindable action SHALL remain unchanged.

#### Scenario: Open from a structured decision

- **WHEN** Aaron selects a structured decision and presses `o`
- **THEN** its coordinator's page opens focused on that decision with board state unchanged

#### Scenario: Open from an FYI-only board

- **WHEN** the board has only FYIs and Aaron presses `o` on one
- **THEN** the page opens with FYIs selected and that FYI focused

#### Scenario: No selection

- **WHEN** open items exist but no row is selected
- **THEN** `o` opens the single-coordinator page without a focus target

#### Scenario: Nothing to answer

- **WHEN** neither decisions nor FYIs are open
- **THEN** a one-line notice says there are no open board items and no browser opens

#### Scenario: API disabled

- **WHEN** Aaron presses `o` while the REST API is disabled
- **THEN** the footer says to enable the API in Settings and no browser opens

#### Scenario: Reply key unchanged

- **WHEN** Aaron presses `r` on any board item
- **THEN** the plain reply prefix is composed exactly as before
