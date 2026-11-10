# Add a selectable dynamic backend

## Why

Argus already supports usage-aware backend tiers, but the router only runs when no task or project backend is selected. Users cannot choose a single `dynamic` backend in defaults or task creation and have Argus resolve it to the configured tier with available usage headroom.

## What changes

- Add a reserved `dynamic` backend selector that resolves through the existing backend-routing tiers.
- Allow `dynamic` as the configured default and as an explicit task backend choice. Project-level `dynamic` choices resolve the same way.
- At task creation, persist and launch the concrete backend selected by the tier resolver. Explicit real backend choices retain their current pinning behavior.
- If no tier can be selected, use the existing configured default backend fallback; reject only if that fallback is also unavailable.
- Keep the current per-tier probe, threshold, account, and routing-strategy semantics.

## Non-goals

- Dynamically switching a running task's backend as usage changes.
- Changing backend usage probes or tier configuration formats.
- Adding a backend setting to REST or MCP beyond the existing selector surfaces; REST clients can submit `dynamic` using the existing backend field.
- macOS task creation UI changes in this change; the macOS app has no task-creation form today.

## Impact

This changes task-creation resolution and backend selection presentation. The TUI and web task-creation surfaces must permit the reserved selector. Existing REST task creation should accept it. macOS is assessed and named above as a non-goal because it does not create tasks.
