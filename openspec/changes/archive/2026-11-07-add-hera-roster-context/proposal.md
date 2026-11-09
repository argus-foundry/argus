# Proposal: context measurements in the Hera roster

## Why
Web and Mac need one roster request for team context pressure; per-task composer reads cannot give a consistent fleet signal.

## What changes
- `GET /api/hera` roles gain `context`: null when no valid observation, else `tokens`, `denominator_tokens`, `denominator_kind` (`worker_window` / `coordinator_recycle_budget`), `percent` (0-100 clamped), `observed_at`.
- Percentage policy comes from `tiering.ContextFor` / `ContextPercent` (shared with the TUI, thresholds unchanged).
- A valid token count with a nonpositive budget keeps `tokens` and returns null `denominator_tokens` / `percent`.
- `observed_at` is always null: `context_size` is a scalar with no producer timestamp, and request or status time is never substituted.

## Deviations
- The model carries 0 for both absent and unparseable `context_size`, so a stored 0 also yields `context: null`.

## Non-Goals
- No new context producer or SSE event. Web adoption is the M3 ticket ENG-1101 (Show measured context pressure on Hera rows).
