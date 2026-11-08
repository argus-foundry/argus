## ADDED Requirements

### Requirement: Web terminal keeps the user's place and draft across a resize kick

The web terminal SHALL report `scrolled_up` (the xterm viewport is above the bottom) and `composer_dirty` (a typed-since-submit fold over bytes sent to the agent: printable and pasted content set it; Enter, Ctrl+C and Ctrl+U clear it; escape sequences, other control bytes and DEL are ignored) on every resize POST. When the response carries `deferred`, the client SHALL re-POST the current size when the viewport returns to the bottom and every 10 seconds, and SHALL stop when the deferral clears, a kick lands, the selected task changes, the page is hidden or the terminal view is torn down. The shell asset change SHALL bump the service worker version.

#### Scenario: Reading history during a resize

- **WHEN** the user is scrolled up and the pane width changes past the margin
- **THEN** the session is not restarted until the viewport is back at the bottom

#### Scenario: Typed but unsent text

- **WHEN** the user has typed in the terminal and not pressed Enter
- **THEN** the resize is deferred and retried until the text is submitted or cleared

#### Scenario: View goes away

- **WHEN** the user switches task or the page is hidden while a deferral is pending
- **THEN** the retry timer is cancelled and no further resize is posted
