## 1. Standing orchestrator

- [ ] 1.1 Add `EnsureResidentOrchestrator` (orchestrator `resident`, coordinator role `resident`, binding to the resident task, one transaction, idempotent); call it from the resident manager before first launch.
- [ ] 1.2 Reserve the orchestrator name `resident` in every orchestrator-creation path (TUI form, `hera_new_orchestrator`, REST); surface a collision as the resident state `Reason`.
- [ ] 1.3 Exclude the orchestrator, role and binding from the Projects tab model, jump switcher, `/api/hera*` roster, cleanup/merge-safety roll-ups and cascade-nuke candidates; reject generic nuke/hide/detach/move on them; extend `TestResidentExcludedEverywhere`.
- [ ] 1.4 Startup reconciliation, finished-session reaper and orphan sweep treat the resident binding as live (tests).

## 2. Spawn and delete

- [ ] 2.1 Require and validate `project` for resident-caller `hera_spawn_worker`; stamp `task_meta(hera, resident_worker)` in the spawn `AfterPersist` hook.
- [ ] 2.2 Extract `mergesafety.ClassifyTask(ctx, task)` from `internal/api/cleanup_candidates.go` so cleanup and delete share candidate construction; add a local "empty work" check (no commits ahead, clean tree, fails closed).
- [ ] 2.3 Implement `hera_delete_worker(role, force?)` over `hera.NukeSingleRole` with the three-layer guard (connection-bound caller only, own-orchestrator worker only, never itself); hide it from non-resident tool listing; no REST/TUI entry point.
- [ ] 2.4 Tests: non-resident coordinator/worker refused, self-reported cwd/id ignored, cross-orchestrator refused, self refused, merged/empty deleted, unmerged refused, force deletes, classifier failure refuses.

## 3. Hera tool policy and orientation

- [ ] 3.1 Update `[resident]` defaults: allow/ask/deny per design D9; extend `resident_inventory_test.go` to classify every hera tool.
- [ ] 3.2 Add the resident-caller fail-closed check at the top of `hera_new_orchestrator` and the reserved-name check for other callers; tests for resident, reserved name and unchanged regular behavior.
- [ ] 3.3 Update the resident orientation (team, spawn with explicit project, which inbox to read, how to answer or relay worker questions, when to delete and when to force) and the resident-worker orientation (ask via `hera_send(status=blocked)`).

## 4. Unattended workers

- [ ] 4.1 Add `Family.UnattendedWorker` to `internal/agent/backends.go` (Claude: bypass-active flags plus `--disallowedTools AskUserQuestion EnterPlanMode ExitPlanMode`); `BuildCmd` applies it for marked tasks only when `argusWrapsSandbox`, else refuses; pre-flight the same check in the spawn path so no half-started task is created.
- [ ] 4.2 Add the eligibility filter to resident-worker backend routing (mirror the Claude-only coordinator filter in `hera_spawn.go`); reject explicit ineligible backends; reject when none is eligible.
- [ ] 4.3 Spike: confirm on a real Codex session whether its default `--dangerously-bypass-approvals-and-sandbox` command can still raise a user-input prompt; mark Codex eligible only if closed, record the result in `backends.md` and a gotcha.
- [ ] 4.4 Needs-input backstop: on a resident worker's flag raise (detector or self-reported blocked) send the resident one Hera message per episode (edge-triggered, exactly-once notifier); log under `[resident]`.
- [ ] 4.5 Tests: sandbox-off refusal on spawn and restart, flags applied only when wrapped, ordinary workers unaffected, once-per-episode notify, eligibility filter cases.

## 5. Recycle and lifecycle

- [ ] 5.1 Route `HeraRecycleRunner.Restart` for `IsResident()` to the resident restart path; share the in-flight guard; build the team section into the resident seed.
- [ ] 5.2 Make the coord-hook's resident path independent of Hera-role presence; resident budget from `resident.context_budget`; no generic pending-recycle flag for the resident.
- [ ] 5.3 Lifecycle: disable/prerequisite-stop leaves workers running; Settings shows the running-worker count; tests for recycle, crash, bounce and disable with live workers.
- [ ] 5.4 Test that `task_recycle` and a forced coordinator recycle fired together produce exactly one restart.

## 6. Resident view

- [ ] 6.1 Add a resident mode to `HeraPage` (single standing orchestrator, resident pane on the coordinator row, worker pane on worker rows, `i` inbox with unread count).
- [ ] 6.2 Enforce the navigation-only key allowlist in resident mode via the keymap (data, not literals); help overlay and ctrl+k palette generated from it; update `help_test.go`.
- [ ] 6.3 Smoke tests: each creation/structure key inert, focus stays put on click, team rail renders needs-input marker, resident-pane lifecycle unchanged (no size-drift kick).

## 7. Docs and parity

- [ ] 7.1 README Reference: `hera_delete_worker`, `[resident]` hera tiers; `gotchas/resident.md`, `gotchas/hera-view.md`, `backends.md` (unattended-worker row); knowledge index rows.
- [ ] 7.2 Record the named web and macOS follow-ups; if the roster exclusion changes any REST field, bump `SW_VERSION` only if static assets change.
- [ ] 7.3 `make pre-pr` clean; archive the change into base specs in the same PR before merge.
