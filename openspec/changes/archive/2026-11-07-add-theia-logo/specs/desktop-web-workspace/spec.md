## MODIFIED Requirements

### Requirement: Standalone additive workspace
The workspace SHALL run as a separate loopback frontend using existing authenticated REST and SSE operations through its development proxy. It SHALL NOT require Go, daemon, existing static shell, REST contract, schema or backend behavior changes. The existing web UI SHALL remain available and unchanged.

#### Scenario: Run alongside the existing UI
- **WHEN** the standalone client is started
- **THEN** it SHALL be served on its own loopback port and forward existing authenticated `/api/` operations with proxy security filtering to one configured loopback daemon
- **AND** response headers including `X-Output-Total` SHALL remain available to the client
- **AND** the existing web UI and daemon remain untouched

#### Scenario: Preserve application identity
- **WHEN** the optional workspace loads
- **THEN** the tab title SHALL be Theia and the header SHALL display the selected Spectrum eye mark at 25 pixels beside the Theia wordmark, with no workspace edition label. The mark SHALL be decorative and the brand accessible name SHALL remain Theia
- **AND** the workspace SHALL serve its own /theia-favicon.svg, /theia-icon-32.png, /theia-icon-192.png and /theia-apple-touch-icon.png (180 pixels) read-only, using these assets for favicon, touch and notification icons without changing the original Argus assets
- **AND** compact header widths SHALL retain their existing responsive visibility and fit without horizontal overflow

### Requirement: Browser attention parity

The workspace SHALL reuse its existing favicon links, showing the Theia Spectrum eye SVG with 32 and 192 pixel PNG fallbacks and the title Theia when no tasks need input. With N tasks needing input, the title SHALL be (N) Theia and the Theia icon SHALL retain its eye with a small count badge composited over it; failed image loading SHALL retain the plain Theia icon. Returning to zero SHALL restore the plain icon and title, including when image loading completes afterward. The workspace SHALL use the task rail as the sole task list. Needs-input alerts SHALL default on in the page, idle alerts SHALL default off. Desktop alerts SHALL use one explicit Web Push permission/subscription control; the page SHALL NOT construct a second desktop Notification. Notifications SHALL ignore focused tasks, dedupe pending task IDs until attention clears and post the first three distinct arrivals immediately within a rolling five-second window. Further arrivals SHALL update one summary. Snapshots SHALL update badges silently. Disconnect SHALL clear alerts, notifications, timers and badge counts. Needs-input and idle in-page preferences SHALL live in Settings Notifications using their existing persisted flags; the removed browser-banner preference SHALL be ignored. There SHALL be no separate attention task popover. Native Dock counts SHALL use tab badges; the native menu-bar extra is not applicable to a browser. Next-attention navigation SHALL respect current rail filters.

#### Scenario: Burst coalescing

- **WHEN** five distinct unfocused tasks need input within five seconds
- **THEN** the first three alerts appear immediately and the remaining two update one summary
- **AND** repeated events for a pending task add no alert

#### Scenario: Theia attention favicon lifecycle

- **WHEN** tasks need input
- **THEN** every favicon candidate SHALL show the Theia eye with the existing 192 pixel canvas badge geometry, capped at 99
- **WHEN** attention returns to zero or icon loading fails
- **THEN** the original Theia favicon links SHALL be restored, including after delayed image loading
- **AND** notification icon and badge SHALL use /theia-icon-192.png without changing the push worker URL or scope
