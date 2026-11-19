# Board decision page specification

## Purpose

Let a human answer a coordinator’s open board decisions as one round on a daemon-served page, with durable answers and one summarized coordinator message.

## Requirements

### Requirement: Daemon-served decision page

The daemon SHALL serve a standalone page at `GET /decide` from its REST server, without authentication because the page shell contains no data, listed as an exact path in the auth middleware's skip list. The page SHALL make no request to any host other than the daemon that served it. It SHALL render all text with text nodes (never `innerHTML`), supporting paragraphs, `- ` and `1. ` lists, paired `**bold**` spans and backtick code spans in situation text. It SHALL be usable at phone width and in light and dark themes.

#### Scenario: Shell without data

- **WHEN** a client requests `/decide` with no credentials
- **THEN** the page shell is served and contains no board data, item ids or tokens

#### Scenario: No third-party requests

- **WHEN** the page loads
- **THEN** it issues no request to any external host, including web-font hosts

#### Scenario: Hostile board text

- **WHEN** an item subject, body, option or note contains markup such as `<img onerror=…>`
- **THEN** it is displayed as literal text and nothing executes

### Requirement: Scoped launch and session

The page SHALL read a launch ticket from the URL fragment, remove the fragment from the address bar and history entry with `history.replaceState` before any network request, exchange the ticket once for a decision session, and keep the session in `sessionStorage`. It SHALL send the session only to the decisions and answers endpoints. When the session is missing, expired or rejected it SHALL show that the link expired and instruct the user to press the board's decide key again, SHALL keep any drafts, and SHALL still offer Copy answers. Neither the ticket nor the session SHALL appear in any URL query string, `localStorage`, daemon log or `uxlog` line.

#### Scenario: Fragment removed

- **WHEN** the page loads with `#t=<ticket>`
- **THEN** the address bar no longer shows the fragment once the page has started its exchange and the browser history entry holds no ticket

#### Scenario: Expired link

- **WHEN** the session is rejected with 401
- **THEN** the page tells the user to reopen from Argus, keeps the drafted answers and offers Copy answers

#### Scenario: Reload

- **WHEN** the user reloads the page within the session lifetime
- **THEN** the page reuses the stored session and drafts without a ticket

### Requirement: Round contents and layout

The round SHALL be every open `decision` item on one coordinator's board in ascending id order; `fyi` and resolved items SHALL NOT appear. A structured decision SHALL render as a card with its subject as the question, its body as the situation, one selectable option card per option (label, summary, Upside and Downside rows), a Recommended pill on the recommended option with the reason, a dashed "Something else" card, and an optional note field. A plain decision SHALL render as a card with its subject, its body, and a note field that is required to answer. The page SHALL show a sticky tally ("N of M decided") with one jump segment per decision, a "Take the recommendation for the N undecided" button when any undecided card has a recommendation, and a Submit answers button enabled only when at least one card is decided and every "Something else" or plain card has a non-empty note. A `focus` item id in the URL SHALL scroll to and highlight that card without hiding the others.

#### Scenario: Mixed round

- **WHEN** the board has two structured decisions and one plain decision
- **THEN** the page shows three cards, two with option cards and a Something else card, one free-text card, and a tally of "0 of 3 decided"

#### Scenario: Something else needs a note

- **WHEN** a card has Something else selected and an empty note
- **THEN** that card shows a hint that a note is needed and Submit is disabled for it

#### Scenario: Take the recommendation

- **WHEN** the user presses the take-the-recommendation button with three undecided cards, two of which have recommendations
- **THEN** only those two cards are marked decided and the third stays undecided

#### Scenario: Focused card

- **WHEN** the page opens with `focus=40`
- **THEN** card 40 is scrolled into view and highlighted and the other open decisions remain listed

### Requirement: Partial submit and per-item results

Submit SHALL send only decided cards, with each card's `expected_updated_at` from when it was loaded. Undecided cards SHALL remain open on the board and on the page. The page SHALL apply each returned outcome to its card: `resolved` cards become read-only "Answered" cards; a `conflict` card is replaced by the item's current content with the user's draft note kept and the choice cleared, and flagged as changed by the coordinator; `already_resolved` and `not_found` cards are marked closed; `invalid` cards show their error. The page SHALL NOT report overall success unless at least one card resolved, and SHALL state how many resolved and how many need attention.

#### Scenario: Partial submit

- **WHEN** the user decides 3 of 5 cards and submits
- **THEN** three items are resolved with the chosen option and note, two stay open, and the page shows 3 answered and 2 undecided

#### Scenario: Coordinator edited a card mid-round

- **WHEN** the coordinator revised a decision after the page loaded and the user submits an answer for it
- **THEN** that answer is not recorded, the card refreshes to the revised content with the user's note preserved, and the other answers in the submission still resolve

#### Scenario: Item answered elsewhere

- **WHEN** an item was dismissed in the TUI after the page loaded and the user answers it on the page
- **THEN** the result is `already_resolved` and the card is marked closed

### Requirement: Durable answers

Each resolved answer SHALL set the item `resolved`, `resolved_by` to `human`, `resolution_choice` to the chosen option id or `other`, and `resolution_note` to the note, bump `updated_at`, and survive daemon restarts and coordinator recycles. A choice SHALL be a declared option id or `other`; `other` SHALL require a non-empty note and a plain decision SHALL accept only `other`. A resident-approval item SHALL NOT be answerable through this path.

#### Scenario: Option recorded

- **WHEN** the user answers a structured decision with option `auto-retry` and a note
- **THEN** the stored item has `resolution_choice` `auto-retry`, `resolution_note` the note and `resolved_by` `human`

#### Scenario: Unknown option rejected

- **WHEN** an answer names an option id not declared on the item
- **THEN** the outcome is `invalid` and the item stays open

#### Scenario: Something else without a note

- **WHEN** an answer uses `other` with an empty note
- **THEN** the outcome is `invalid` and the item stays open

### Requirement: One summary message to the coordinator

When a submission resolves at least one item, the system SHALL write exactly one self-addressed coordinator message in the same transaction as the item resolutions, delivered through the existing board-notice outbox and shown as `Aaron (board)`. It SHALL list, for each resolved item in id order, the id, subject, the chosen option label (marking when it is the recommendation, or "Something else") and the note or `none`, followed by the ids of the coordinator's items that remain open. It SHALL state that every listed item is already resolved. A submission that resolves nothing SHALL write no message. Resolving through this path SHALL NOT also write the human-dismissal "dropped" notice. Full notes up to 4 KiB SHALL remain durably stored on each item. The summary SHALL fit within 64 KiB by budgeting note space evenly across answered items. Each shortened note SHALL end with `… [truncated — full note on board item #N; hera_board_list include_resolved=true]`; notes under budget SHALL remain untouched. The notice SHALL remain durable and deliver after a binding exists if none is live.

#### Scenario: One message for a round

- **WHEN** a submission resolves four items
- **THEN** exactly one message is written listing the four answers and the remaining open ids, and the coordinator receives it once

#### Scenario: Atomic with resolution

- **WHEN** the process exits between answering and delivery
- **THEN** the resolved items and the pending message both exist and the notify tick delivers the message later

#### Scenario: No message when nothing resolved

- **WHEN** every answer in a submission conflicts
- **THEN** no message is written

#### Scenario: Maximum-size notes

- **WHEN** sixteen answers each contain a 4 KiB note
- **THEN** full notes remain on the items and the summary fits within 64 KiB, with every shortened note carrying the full-note pointer

#### Scenario: Coordinator unbound

- **WHEN** a submission resolves items while the coordinator has no live binding
- **THEN** the message stays pending and is delivered once a binding exists

### Requirement: Live change awareness and drafts

While visible, the page SHALL poll the decisions endpoint every 20 seconds. When items were added, revised or resolved elsewhere it SHALL show a banner with a Refresh action and SHALL NOT replace or reorder any card the user is editing. Drafts (choice and note) SHALL persist in `localStorage` keyed by role, item id and the item's `updated_at`, so a draft is discarded when the coordinator revises that item and survives a reload or an expired session. Copy answers SHALL produce a plain-text summary of the current choices and notes.

#### Scenario: New decision arrives

- **WHEN** the coordinator posts a new decision while the page is open
- **THEN** a banner offers Refresh and no existing card changes until the user accepts

#### Scenario: Revised item discards its draft

- **WHEN** the coordinator revises an item and the user refreshes
- **THEN** the card shows the new content with no stale choice, while drafts for unrevised items remain

#### Scenario: Copy fallback

- **WHEN** the user presses Copy answers
- **THEN** a text block listing each decision, its choice and its note is copied or shown selected for manual copying

### Requirement: Observability

The launch, exchange, read and answer paths SHALL log under the `[board]` and `[decide]` prefixes with role id, item ids, counts and outcomes, including rejected tickets, expired sessions, per-item conflicts and skipped work. No log line SHALL contain a ticket, a session, a note or a body.

#### Scenario: Conflict is logged

- **WHEN** an answer returns `conflict`
- **THEN** a `[decide]` line records the role id, item id and outcome and contains no note text

#### Scenario: Secrets never logged

- **WHEN** a ticket is minted, exchanged and a session is used
- **THEN** no log line contains either value

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
