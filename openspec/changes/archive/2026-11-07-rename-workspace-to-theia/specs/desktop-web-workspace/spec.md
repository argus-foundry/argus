## MODIFIED Requirements

### Requirement: Browser attention parity

The workspace SHALL reuse the existing favicon link, showing /icon-192.png and the title Theia when no tasks need input. With N tasks needing input, the title SHALL be (N) Theia and the original icon SHALL retain its shield with a small count badge composited over it; failed image loading SHALL retain the plain original icon. Returning to zero SHALL restore the plain icon and title, including when image loading completes afterward. The workspace SHALL use the task rail as the sole task list. Needs-input alerts SHALL default on in the page, idle alerts SHALL default off. Desktop alerts SHALL use one explicit Web Push permission/subscription control; the page SHALL NOT construct a second desktop Notification. Notifications SHALL ignore focused tasks, dedupe pending task IDs until attention clears and post the first three distinct arrivals immediately within a rolling five-second window. Further arrivals SHALL update one summary. Snapshots SHALL update badges silently. Disconnect SHALL clear alerts, notifications, timers and badge counts. Needs-input and idle in-page preferences SHALL live in Settings Notifications using their existing persisted flags; the removed browser-banner preference SHALL be ignored. There SHALL be no separate attention task popover. Native Dock counts SHALL use tab badges; the native menu-bar extra is not applicable to a browser. Next-attention navigation SHALL respect current rail filters.

#### Scenario: Burst coalescing

- **WHEN** five distinct unfocused tasks need input within five seconds
- **THEN** the first three alerts appear immediately and the remaining two update one summary
- **AND** repeated events for a pending task add no alert

