## ADDED Requirements

### Requirement: Board `o` opens the decision page in the browser

With the coordinator board focused, the bound `hera_board.decide` key (default `o`) SHALL ask the daemon for a launch ticket for the board's coordinator role and open the daemon-served decision page in the default browser, passing the selected item as the page's focus. It SHALL work on any selected row, including `fyi` rows, and with no selection. It SHALL NOT change selection, expansion, read state, focus or the coordinator composer. If the board has no open `decision` items it SHALL show a one-line notice and open nothing. If the REST API is not enabled it SHALL show a notice telling the user to enable it in Settings and open nothing. In `--remote` mode, where the Hera page is inert, the key SHALL do nothing. The action SHALL log under the `[board]` prefix without logging the ticket.

#### Scenario: Open from a structured decision

- **WHEN** Aaron focuses the board, selects a structured decision and presses `o`
- **THEN** the browser opens the decision page for that coordinator focused on that item and the board state is unchanged

#### Scenario: Open from a plain or fyi row

- **WHEN** Aaron presses `o` on a `fyi` row while a plain decision is open
- **THEN** the page opens with that decision's card present and no card focused

#### Scenario: Nothing to answer

- **WHEN** Aaron presses `o` and the board has only `fyi` items or no items
- **THEN** a one-line notice says there are no open decisions and no browser is opened

#### Scenario: API disabled

- **WHEN** Aaron presses `o` and the REST API is not enabled
- **THEN** the footer says to enable the API in Settings and no browser is opened

#### Scenario: Reply key unchanged

- **WHEN** Aaron presses `r` on any board item
- **THEN** the plain reply prefix is composed exactly as before
