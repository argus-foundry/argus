## Implementation

- [x] Add reserved dynamic selection handling to shared backend resolution, including default, project, and explicit task selection.
- [x] Ensure task creation persists the selected concrete backend and preserves account routing semantics.
- [x] Update TUI and web backend selectors to expose `dynamic` where backend selection is offered.
- [x] Add focused tests for dynamic default, explicit, project, fallback, concrete pin paths, API listing, and selector behavior.
- [x] Update existing backend-tier routing docs and record any non-obvious gotchas.
- [x] Assess REST and macOS parity; document any deliberate surface gap in the change.
