## Context

The gater derives a base from blockers, an orchestrator override, or its coordinator task, then sends it to either materialization path. Git resolution currently permits a foreign unresolved branch to reach worktree creation.

## Goals / Non-Goals

**Goals:** Use the target default for unsuitable derived bases while retaining valid same-project stacking.

**Non-Goals:** Change explicit spawn-worker input validation, fan-in winner selection, or any frontend API.

## Decisions

- Preserve source-task project provenance for blockers and coordinator fallback. Reject foreign provenance even when a branch name happens to exist in both repos.
- Read the target project path from the database's merged configuration, matching CreateAndStart. An exported agent helper fetches best-effort and checks the same local/upstream/origin resolution used by worktree creation.
- Clear rejected derived branches before the worker/subcoord split, logging the fallback. Empty input lets CreateAndStart select the configured project branch.

## Risks / Trade-offs

- Fetch can delay materialization → use the existing bounded worktree fetch behavior and continue on fetch failure.
- Missing configured repositories reject derived branches → normal creation reports configuration errors instead of inventing a base.
