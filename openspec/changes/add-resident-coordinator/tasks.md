## 1. Standing orchestrator

- [ ] 1.1 Add `EnsureResidentOrchestrator` (orchestrator `resident`, coordinator role `resident`, binding to the resident task, one transaction, idempotent); call it from the resident manager before first launch.
- [ ] 1.2 Reserve the orchestrator name `resident` in every orchestrator-creation path (TUI form, `hera_new_orchestrator`, REST); surface a collision as the resident state `Reason`.
- [ ] 1.3 Exclude the orchestrator, role and binding from the Projects tab model, jump switcher, `/api/hera*` roster, cleanup/merge-safety roll-ups and cascade-nuke candidates; reject generic nuke/hide/detach/move on them; extend `TestResidentExcludedEverywhere`.
- [ ] 1.4 Startup reconciliation, finished-session reaper and orphan sweep treat the resident binding as live (tests).

## 2. Spawn and delete

- [ ] 2.1 Require and validate `project` for resident-caller `hera_spawn_worker`; stamp `task_meta(hera, resident_worker)` in the spawn `AfterPersist` hook.
- [ ] 2.2 Extract `mergesafety.ClassifyTask(ctx, task)` from `internal/api/cleanup_candidates.go` so cleanup and delete share candidate construction; add a local "empty work" check (no commits ahead, clean tree, fails closed).
- [ ] 2.3 Implement `hera_delete_worker(role, force?)` over `hera.NukeSingleRole` with the three-layer guard (connection-bound caller only, own-orchestrator live worker only, never itself); hide it from non-resident tool listing; no REST/TUI entry point; compute the unmerged summary (branch, commits ahead with subjects, dirty count, PR state, `unknown (reason)` fallbacks) before any force nuke and return it in the result.
- [ ] 2.4 Tests: non-resident coordinator/worker refused, self-reported cwd/id ignored, cross-orchestrator refused, self refused, merged/empty deleted, unmerged refused, force deletes, classifier failure refuses, force result carries the unmerged summary (including an `unknown` field case), delete and spawn raise no prompt, delete of a graduated role is rejected with the task untouched.

## 3. Hera tool policy and orientation

- [ ] 3.1 Update `[resident]` defaults: allow (including spawn and delete), ask (`hera_graduate_worker` only), deny per design D9; extend `resident_inventory_test.go` to classify every hera tool.
- [ ] 3.2 Add the resident-caller fail-closed check at the top of `hera_new_orchestrator` and the reserved-name check for other callers; tests for resident, reserved name and unchanged regular behavior.
- [ ] 3.3 Update the resident orientation (team, spawn with explicit project, which inbox to read, how to answer or relay worker questions, when to delete and when to force, REPORT every force delete with the worker name and unmerged content in its own conversation, the human-action notices, how to run a graduation: ask Aaron first, then call the tool) and the resident-worker orientation (ask via `hera_send(status=blocked)`, when to ask for graduation, ask rather than self-promote).

## 4. Unattended workers

- [ ] 4.1 Add `Family.UnattendedWorker` to `internal/agent/backends.go` (Claude: bypass-active flags plus `--disallowedTools AskUserQuestion EnterPlanMode ExitPlanMode`); `BuildCmd` applies it for marked tasks only when `argusWrapsSandbox`, else refuses; pre-flight the same check in the spawn path so no half-started task is created.
- [ ] 4.2 Add the eligibility filter to resident-worker backend routing (mirror the Claude-only coordinator filter in `hera_spawn.go`); reject explicit ineligible backends; reject when none is eligible.
- [ ] 4.0 **Spike FIRST (gates 4.3 and the Codex row of the worker-backends setting):** on a real Codex session, determine whether the default `--dangerously-bypass-approvals-and-sandbox` command can still raise a user-input prompt (structured user-input tool, plan/collaboration choice, approval escalation), which launch switch closes each, and whether it survives `resume`; record transcripts in a gotcha and `backends.md`. If it cannot be closed, Codex stays ineligible and 4.3/4.6 are dropped.
- [ ] 4.3 Implement Codex `UnattendedWorker` from the spike result (family registry only, sandbox gate unchanged, same orientation and needs-input backstop); tests for flags present only when wrapped, refusal when not, survives resume.
- [ ] 4.4 Needs-input backstop: on a resident worker's flag raise (detector or self-reported blocked) send the resident one Hera message per episode (edge-triggered, exactly-once notifier); log under `[resident]`.
- [ ] 4.5 Tests: sandbox-off refusal on spawn and restart, flags applied only when wrapped, ordinary workers unaffected, once-per-episode notify, eligibility filter cases.
- [ ] 4.6 Add `[resident] worker_backends` (default: all eligible) and the Settings Resident → Worker backends row listing only eligible families (config.toml-shadows-Settings marker, ineligible names ignored with a log line); routing and explicit-backend checks use the enabled set; tests for default, ineligible-never-listed, disabled-never-routed, empty set refuses.

## 5. Recycle and lifecycle

- [ ] 5.1 Route `HeraRecycleRunner.Restart` for `IsResident()` to the resident restart path; share the in-flight guard; build the team section into the resident seed.
- [ ] 5.2 Make the coord-hook's resident path independent of Hera-role presence; resident budget from `resident.context_budget`; no generic pending-recycle flag for the resident.
- [ ] 5.3 Lifecycle: disable/prerequisite-stop leaves workers running; Settings shows the running-worker count; tests for recycle, crash, bounce and disable with live workers.
- [ ] 5.4 Test that `task_recycle` and a forced coordinator recycle fired together produce exactly one restart.

## 5b. Graduation

- [ ] 5b.1 Add `db.GraduateHeraWorker` (one transaction: end worker binding with new end reason `graduated`, create top-level orchestrator named from the role and de-collided, create coordinator role with the worker's project and prompt, bind to the same task and worktree, clear dead-role mail; no orchestrator link); atomicity test with an injected failure at each step.
- [ ] 5b.2 Implement `hera_graduate_worker(role)` with the `hera_delete_worker` guard shape, fail-closed preflights (coordinator-capable backend, resumable session id, idle session) and hidden listing; tests for non-resident, self-reported identity, cross-orchestrator, self, non-Claude, busy, no session id.
- [ ] 5b.3 Post-commit restart in place: clear the resident-worker marker, write `graduated_from`, stop stray jobs, wait for idle (bounded), kill-and-resume via the existing primitive (not recycle), record a pending restart on timeout, then send the coordinator orientation message; tests for normal flags after relaunch, same session id/worktree/branch, no seed prompt, timeout path, crash between steps.
- [ ] 5b.4 Reject `hera_new_orchestrator` for resident workers (fail-closed, before the scan) with the graduation hint; test ordinary workers unchanged.
- [ ] 5b.5 Test that `hera_delete_worker` rejects a graduated role and the resident rail drops it; test the graduate appears in the normal rail and `/api/hera`, uses ordinary coordinator recycle, and is not hidden by the resident filters.

## 6. Resident view

- [ ] 6.1 Add a resident mode to `HeraPage` (single standing orchestrator, resident pane on the coordinator row, worker pane on worker rows, `i` inbox with unread count).
- [ ] 6.2 Enforce the resident-mode key allowlist via the keymap (data, not literals): navigation plus nuke, hide, rename and role-status on worker rows only (inert on the resident row), every creation/structure key inert; help overlay and ctrl+k palette generated from it; update `help_test.go`.
- [ ] 6.4 Daemon notice to the resident when a worker of the standing orchestrator is nuked, hidden or renamed by anyone but the resident's own delete (hook the shared reclaim/archive/rename choke points with the actor passed down, covering rail, REST and Tasks-list delete; include unmerged info for a nuke where cheap); no message for role-status steps; tests per action and for no echo of the resident's own delete.
- [ ] 6.3 Smoke tests: each creation/structure key inert, each allowed key acts on worker rows only, focus stays put on click, team rail renders needs-input marker, resident-pane lifecycle unchanged (no size-drift kick).

## 7. Docs and parity

- [ ] 7.1 README Reference: `hera_delete_worker`, `hera_graduate_worker`, `[resident] worker_backends`, `[resident]` hera tiers; `gotchas/resident.md`, `gotchas/hera-view.md`, `backends.md` (unattended-worker row); knowledge index rows.
- [ ] 7.2 Record the named web and macOS follow-ups; if the roster exclusion changes any REST field, bump `SW_VERSION` only if static assets change.
- [ ] 7.3 `make pre-pr` clean; archive the change into base specs in the same PR before merge.
