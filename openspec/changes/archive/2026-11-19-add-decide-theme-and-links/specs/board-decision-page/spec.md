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
