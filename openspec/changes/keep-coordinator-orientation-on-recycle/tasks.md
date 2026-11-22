## 1. Orientation derivation

- [ ] 1.1 Write failing tests in `internal/hera` for `CoordinatorOrientation`: top-level, `kind=subcoord` bridge (with check-in), self-promoted worker bridge (no check-in), non-coordinator (empty), orchestrator lookup failure (empty), binding-list failure (top-level fallback)
- [ ] 1.2 Implement `hera.CoordinatorOrientation(store, role, taskID)` and its narrow `OrientationStore` interface, with `[recycle]`/uxlog logging on fail-open paths

## 2. Recycle seed

- [ ] 2.1 Write failing tests: a top-level coordinator seed contains the orientation before the original-prompt section; it still does after two consecutive recycles (re-seed with task prompt overwritten); a sub-coordinator plan node seed has the sub-coordinator orientation, check-in and "already checked in" line; worker and freelance seeds contain no orientation section
- [ ] 2.2 Add the "Standing coordinator orientation (still in force)" section to `BuildRecycleSeedPrompt` (extend `RecycleStore` as needed)
- [ ] 2.3 Write a failing test that a hera seed starts with `[argus] ` and that `lastexchange.Extract` over a transcript whose only user turn is that seed yields no human message (and a later human reply is still extracted)
- [ ] 2.4 Prefix `BuildRecycleSeedPrompt` with `lastexchange.SystemTurnPrefix`; fix existing tests that assumed the old start

## 3. Plain task recycle

- [ ] 3.1 Write a failing test that `buildTaskRecycleSeedPrompt` starts with `[argus] `; confirm the resident seed test is unchanged
- [ ] 3.2 Prefix `buildTaskRecycleSeedPrompt` with `lastexchange.SystemTurnPrefix`

## 4. hera_new_orchestrator

- [ ] 4.1 Write failing MCP tests: a plain task's success result contains the full coordinator orientation; a self-promoting worker's contains the sub-coordinator orientation; omitted `prompt` stores the task prompt on the role; explicit `prompt` wins
- [ ] 4.2 Append the orientation section to the tool result and fall back `Prompt` to `task.Prompt`

## 5. Verification and docs

- [ ] 5.1 Add a gotcha bullet (orchestration.md) on deriving the coordinator orientation at recycle time and the `[argus] ` marker on recycle seeds; update the index row
- [ ] 5.2 Run `make pre-pr` clean
- [ ] 5.3 Review (hera-review), archive the change into base specs, validate with `openspec validate --all --strict`
