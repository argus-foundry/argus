# Show provider usage in web Settings

## Why

The web app already has cached Claude and Codex quota readings for routing and account display, but Settings does not show the user their current provider limits. Exposing the readings in Settings makes remaining capacity and reset timing visible without starting an agent session.

## What changes

- Add a Usage section to web Settings showing Claude five-hour and weekly windows for configured Claude accounts and the available Codex weekly window.
- Reuse cached daemon readings and expose the Codex reading through an authenticated API response; do not launch extra provider probes just to render Settings.
- Show each window's usage percentage, reset time, and an unavailable state when no current reading exists. Codex currently supplies a weekly reading only; do not fabricate a five-hour Codex window.
- Refresh the displayed readings while Settings is open.

## Non-goals

- Adding new provider probes or changing probe cadence, routing, or quota behavior.
- Showing account usage in TUI or macOS Settings; this change is limited to the web app.
- Displaying absolute request or token counts when the providers do not supply them.

## Risks

- Readings are snapshots and may be absent or stale; the UI must make unavailable data clear and use existing cache validity rules.
- The accounts API currently includes Claude windows. Codex usage needs a narrowly scoped authenticated read surface using the daemon's existing cached snapshot.
