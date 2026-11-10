## Why

An explicit model ID is accepted at task creation and can then be silently replaced by the backend default at launch (`ResolveModel` falls through when `backendAllowsModel` rejects it). Clients cannot tell whether the requested model will run.

## What Changes

- `GET /api/backends` entries carry `model_policy`: `configured` (the backend has a `models` list), `curated` (family `KnownModels`), or `freeform` (no selector; any ID accepted). `models` now reflects the merged `config.toml` overlay.
- Task creation (`POST /api/tasks`, JSON and multipart, and shared `agent.CreateAndStart`) validates a non-empty `model` against the resolved backend before any worktree, DB row or session exists. Rejection is 422 `{"error","code":"model_not_allowed","backend","requested_model","allowed_models"}`.
- New `agent.ModelPolicy`, `agent.ValidateExplicitModel`, `*agent.ModelNotAllowedError` (wraps `agent.ErrModelNotAllowed`).

## Non-Goals

- Web and TUI picker changes: follow-up ENG-1110 (Web: Explain model selection limits before starting a task). TUI gets the rejection through `CreateAndStart` unchanged. macOS must surface the 422; its picker is a named native follow-up.
- Profile-driven model fallback is unchanged.
- `ResolveModel` keeps falling open for already-persisted tasks (restart/kick/recycle cannot fail on a backend config change); strictness lives at creation.
