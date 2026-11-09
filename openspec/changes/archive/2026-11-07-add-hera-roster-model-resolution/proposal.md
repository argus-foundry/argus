# Proposal: resolved model choices in the Hera roster

## Why
REST clients can read requested task settings but not the resolved team model choice the TUI shows (archetype, applied model, effort, profile warning).

## What changes
- `GET /api/hera` roles gain `archetype` and `model_resolution` (`requested_model`, `effective_model`, `effort`, `source`, `basis`, `profile`, `warning`).
- The off-render resolver moves from `internal/tui/hera_tiering.go` into the tview-free `internal/hera/tiering` package; the TUI rail and the REST handler both call it. `agent.ResolveModelDetailed` reports whether a task override drove the result.
- One resolver per snapshot memoizes profile loads; nothing persists across snapshots.

## Deviations
- The TUI previously derived the profile name from the project binding only; the shared resolver also honors an explicit task `Profile`, matching `agent.ResolveModel`.
- A role whose bound task cannot be fetched falls back to planned-intent inputs (logged) instead of failing the roster.

## Non-Goals
- No write route, event, or account/tier editing.
- Web adoption is the M3 ticket ENG-1099 (Show resolved models and archetypes in Info). Mac and TUI controls are unchanged.
