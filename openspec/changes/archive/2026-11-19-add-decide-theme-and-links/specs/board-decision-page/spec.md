## ADDED Requirements

### Requirement: Remembered decision page appearance

The page SHALL expose a visible, labeled Light / Dark / System control. System SHALL be the default and follow prefers-color-scheme, including system changes while open. A valid stored preference SHALL apply before first paint and persist across decision pages using localStorage. Every theme storage read and write, including access to the storage object, SHALL be guarded by try/catch; unavailable storage SHALL NOT prevent rendering or changing appearance. The theme key SHALL contain only a validated theme value and SHALL never contain ticket or session data.

#### Scenario: Remembered appearance

- **WHEN** the user selects Dark and later opens another decision page
- **THEN** it renders dark from first paint and its control shows Dark

#### Scenario: Storage unavailable

- **WHEN** accessing, reading or writing localStorage throws
- **THEN** the page still renders, defaults to System without a readable preference, and permits temporary Light or Dark selection

#### Scenario: System follows the browser

- **WHEN** System is selected and the browser color scheme changes
- **THEN** the page follows that scheme without a reload

### Requirement: Safe decision reference links

HTTP and HTTPS URLs in decision subjects, situation/body, option summary, upside, downside and recommendation reason SHALL render as clickable anchors with target="_blank" and rel="noopener noreferrer". Other schemes including javascript and data SHALL remain text. Terminal sentence punctuation such as periods, commas and unmatched closing parentheses SHALL remain outside the destination. Links SHALL be built using DOM APIs and textContent or text nodes, never innerHTML with decision text. Existing paragraphs, lists, bold and backtick code rendering SHALL remain supported; code spans SHALL NOT contain generated links. Hostile HTML SHALL remain text. Rendering links SHALL NOT issue external requests or weaken launch fragment stripping or credential storage boundaries.

#### Scenario: References and punctuation

- **WHEN** decision text includes `https://example.com/path).` or `http://example.com,`
- **THEN** HTTP(S) references are links and trailing punctuation remains visible outside each link

#### Scenario: Code and unsafe schemes

- **WHEN** decision text includes a backtick-wrapped URL, a javascript URL or a data URL
- **THEN** each remains literal text with no generated anchor

#### Scenario: Hostile reference text

- **WHEN** a reference is followed by an HTML event-handler payload
- **THEN** the payload stays literal text and no handler executes or external resource loads

### Requirement: Finish successful submissions

After a submission where every submitted item returns resolved or already_resolved, the page SHALL attempt window.close. If the window remains open or closing throws, it SHALL replace the decision content with “Answers sent — you can close this tab” and the number of submitted items answered. Undecided items excluded from the submission SHALL NOT block completion and SHALL remain open on the board. The page SHALL NOT attempt to close or show the sent confirmation if any submitted outcome is conflict, invalid or not_found, if any submitted item has no matching result, or if the request fails. In those cases the existing results and kept drafts SHALL remain available. Completion SHALL prevent polling or late responses from updating removed controls.

#### Scenario: Every submitted answer accepted

- **WHEN** every submitted item returns resolved or already_resolved, with other undecided items on the board
- **THEN** the tab attempts to close and those undecided items stay open on the board

#### Scenario: Browser refuses closing

- **WHEN** every submitted item is accepted but the browser keeps the tab open or closing throws
- **THEN** the page replaces the decisions with “Answers sent — you can close this tab” and the answered count

#### Scenario: Submission needs attention

- **WHEN** any submitted item conflicts, is invalid or not_found, has no matching result, or the request fails
- **THEN** the tab stays open without attempting to close and the existing result handling and kept drafts remain available

## MODIFIED Requirements

### Requirement: Partial submit and per-item results

Submit SHALL send only decided cards, with each card's `expected_updated_at` from when it was loaded. Undecided cards SHALL remain open on the board and SHALL NOT be submitted. When every submitted item returns `resolved` or `already_resolved`, the page SHALL finish the successful submission even when other cards remain undecided. Otherwise the page SHALL remain open and apply each returned outcome to its card: `resolved` cards become read-only "Answered" cards; a `conflict` card is replaced by the item's current content with the user's draft note kept and the choice cleared, and flagged as changed by the coordinator; `already_resolved` and `not_found` cards are marked closed; `invalid` cards show their error. For submissions that remain open, the page SHALL NOT report overall success unless at least one card resolved, and SHALL state how many resolved and how many need attention.

#### Scenario: Partial submit

- **WHEN** the user decides 3 of 5 cards and all three submitted answers resolve
- **THEN** three items are resolved with the chosen option and note, two stay open on the board, and the tab attempts to close or shows the sent confirmation with 3 items answered

#### Scenario: Coordinator edited a card mid-round

- **WHEN** the coordinator revised a decision after the page loaded and the user submits an answer for it
- **THEN** that answer is not recorded, the card refreshes to the revised content with the user's note preserved, and the other answers in the submission still resolve

#### Scenario: Item answered elsewhere

- **WHEN** an item was dismissed in the TUI after the page loaded and the user answers it on the page
- **THEN** the result is `already_resolved`; the page finishes if every submitted item is accepted, otherwise that card is marked closed on the page that remains open

### Requirement: Live change awareness and drafts

While visible and before a submission finishes successfully, the page SHALL poll the decisions endpoint every 20 seconds. When items were added, revised or resolved elsewhere it SHALL show a banner with a Refresh action and SHALL NOT replace or reorder any card the user is editing. Drafts (choice and note) SHALL persist in `localStorage` keyed by role, item id and the item's `updated_at`, so a draft is discarded when the coordinator revises that item and survives a reload or an expired session. While the decision page remains unfinished, Copy answers SHALL produce a plain-text summary of the current choices and notes.

#### Scenario: New decision arrives

- **WHEN** the coordinator posts a new decision while the unfinished decision page is open
- **THEN** a banner offers Refresh and no existing card changes until the user accepts

#### Scenario: Revised item discards its draft

- **WHEN** the coordinator revises an item and the user refreshes
- **THEN** the card shows the new content with no stale choice, while drafts for unrevised items remain

#### Scenario: Copy fallback

- **WHEN** Copy answers completes while the decision page is unfinished
- **THEN** a text block listing each decision, its choice and its note is copied or shown selected for manual copying
