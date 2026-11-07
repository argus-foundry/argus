## Why

Routing (project override, tiered usage routing, accounts, profiles) resolves only at task creation, so a web client cannot offer a reliable model choice while the backend is inherited.

## What Changes

- `POST /api/routing/preview` returns the backend, kind, selectable models, model policy, default model and routing reason that creation would resolve. Read-only and advisory.
- The preview calls `agent.ResolveTaskRouting` with `reserve=false`, the same resolver `CreateAndStart` uses, so the two cannot drift.
- Creation keeps revalidating an explicit model against the final routing result (already enforced by `validate-explicit-models`).

## Non-Goals

- No web or native UI. Follow-up: ENG-1111 (Web: Choose models with an inherited backend); the Mac app may adopt the endpoint later. The TUI resolves routing during shared creation and needs no change.
- No account selection input and no account identifiers in the response.

## Deviations

- `routing_reason` carries the resolver's reason string (strategy, probe, usage, threshold, or the fallback note) rather than a fixed label such as "project override".
- An unknown explicit backend or an unusable account answers 422 `backend_unavailable`.
