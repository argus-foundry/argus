## ADDED Requirements

### Requirement: Decision page bypasses the service worker cache

The service worker SHALL treat `/decide` as network-only, never serving it from or writing it to the shell cache, so a stale page or a ticketed URL is never cached. `/decide` SHALL NOT be listed in the cached shell assets. Because the worker script changes, `SW_VERSION` SHALL be bumped in the same change.

#### Scenario: Navigation to the decision page

- **WHEN** the browser navigates to `/decide` with the service worker active
- **THEN** the request goes to the network and no cache entry is created for it

#### Scenario: Version bump

- **WHEN** this change ships
- **THEN** `SW_VERSION` is `argus-shell-v95` so installed PWAs pick up the new worker
