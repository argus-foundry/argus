## Why

Web users cannot inspect or reorder the backend tiers that route new tasks, or see the live usage each tier is scored with (ENG-1047).

## What Changes

- `GET /api/backend-tiers` returns `{source, editable, revision, tiers[]}`; each tier carries `backend`, `account`, `probe`, `threshold_pct` and `usage` (null when routing has no reading).
- `PUT /api/backend-tiers` replaces the whole list transactionally. It requires `If-Match` (the GET revision) and validates every tier before storing anything.
- `GET /api/backend-tiers/usage` returns `{revision, readings:[{index, usage}]}`.
- `config.changed {"resource":"backend_tiers"}` after a committed save; new `backend_tiers.usage_changed {"revision"}` when routed readings change.
- `internal/backendtier` gains the shared projection (`ProjectTierUsage`, `TierRevision`, `TierUsageFingerprint`) that mirrors `ResolveChoice`'s account selection and provider scope.

## Non-Goals

- No Web, TUI or macOS UI. Follow-ups: ENG-1088 (Web: arrange backend tiers and inspect usage in Settings), ENG-1095 (TUI: see current usage beside backend tier settings).
- No edits to `strategy`, the costed Codex probe toggle, or Hera-specific policy. Routing behavior is unchanged.
- No config.toml writes: a config.toml-owned list is read-only.
