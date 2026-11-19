## ADDED Requirements

### Requirement: Direct Decide entry in the web app

The web app SHALL provide an always-available global Decide entry alongside its main navigation with a badge showing the total open decision and FYI count across eligible coordinators. It SHALL fetch counts with its existing master/device token in the authorization header on app entry, foreground return and existing visible polling cadence. Resolved items SHALL not count; read FYIs SHALL count. A failed count fetch SHALL show unavailable/stale state rather than a false zero. At zero the entry SHALL still open an empty page. The entry SHALL be usable and labeled at phone width without navigation overflow.

Activating Decide SHALL request an aggregate launch ticket using the existing credential in an API header, then navigate in the current browsing context to same-origin `/decide#t=<ticket>`. The device/master token SHALL never enter a URL, fragment, history entry or decision-page session storage. Launch failure or offline state SHALL remain in the app with retry feedback and SHALL not open an unauthenticated empty page. This flow SHALL require no TUI interaction or prior coordinator selection. Expiry and successful-close fallback SHALL provide a same-origin Return to Argus path.

#### Scenario: Phone-only launch

- **WHEN** Aaron taps Decide in the authenticated installed PWA with open items on two coordinators
- **THEN** one grouped page opens with Decisions/FYIs counts without pressing `o` or choosing a coordinator

#### Scenario: FYI-only count

- **WHEN** only three read, open FYIs exist
- **THEN** the entry badge shows 3 and launches with FYIs selected

#### Scenario: Empty queue and failed counts

- **WHEN** the queue is empty
- **THEN** the entry remains available with a zero count; a failed later count request marks the count unavailable/stale rather than asserting zero

#### Scenario: No token in navigation

- **WHEN** a phone launch succeeds
- **THEN** the launch request carries the device credential only in its header and the navigation carries only a single-use fragment ticket stripped before exchange

#### Scenario: Offline launch

- **WHEN** the launch request cannot reach the daemon
- **THEN** the PWA remains visible with retry feedback and no page session is fabricated

### Requirement: Decide shell updates preserve cache and credential boundaries

The service worker SHALL continue treating `/decide` as network-only and outside `SHELL_ASSETS`, and SHALL NOT cache decision data, count/session responses or mutation responses. Changes to any shell asset under `internal/api/static/` SHALL include a `SW_VERSION` bump in the same implementation change so installed PWAs receive the updated navigation and page. Polling SHALL remain the update mechanism; no live push dependency SHALL be added.

#### Scenario: Installed PWA updates

- **WHEN** the implementation changes the PWA entry or decide page
- **THEN** SW_VERSION advances from its then-current value and `/decide` and credential-bearing API responses remain outside the cache
