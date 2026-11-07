# Standalone web workspace tasks

## 1. Study

- [x] 1.1 Record the standalone frontend scope in the proposal and client contracts.
- [x] 1.2 Reconcile active requirements to additive standalone frontend work only.
- [x] 1.3 Complete local UI implementation without changing Go, daemon, existing static assets, REST contracts or schema.
- [x] 1.4 Keep the existing web UI available and unchanged, provide integrated Settings and task creation while excluding unsupported Hera mutations.

## 2. Frontend

- [x] 2.1 Serve the client on a separate loopback port and proxy `/api/` unchanged to one configured loopback daemon, preserve `X-Output-Total`.
- [x] 2.2 Add project grouped task navigation, independent rails, agent focus and bounded resizable panels.
- [x] 2.3 Keep drafts in memory per task and guard browser task switching and stale UI responses without claiming stronger backend guarantees.
- [x] 2.4 Show the real raw agent terminal, existing changed and untracked paths, unified and split diffs, registered artifact previews and task status.
- [x] 2.5 Fit visible idle terminals after settled geometry under fresh eligibility checks, retain explicit Fit as fallback.
- [x] 2.6 Preserve existing archive and restore effects, explain consequences and confirm mutations.
- [x] 2.7 Keep drafts in memory and credentials out of logs, support opt-out remembered credentials and sandbox artifact previews.

## 3. Verification

- [ ] 3.1 Verify the browser flow against an isolated test daemon, not the live daemon.
- [x] 3.2 Verify proxy headers including `X-Output-Total`, API/SSE access, task switching, drafts, diffs, artifacts and manual Fit session behavior.
- [x] 3.3 Document client limits and named follow-ups.

## 4. Remaining acceptance

- [ ] 4.1 Review the running optional client with the user.
- [ ] 4.2 Resolve or explicitly accept the unchanged daemon regression failure before publication.
- [x] 4.3 Address initial dogfooding feedback for composer focus, paste routing, Enter and Shift+Enter, with isolated browser and policy regressions.
- [x] 4.4 Handle native plaintext paste at the selected range and capture the composer draft explicitly.

## 5. Terminal recovery and project navigation

- [x] 5.1 Prevent ordinary reconnect from stranding rerender recovery after same-task offline/live refreshes.
- [x] 5.2 Decode git-quoted directory entries before requesting their diffs.
- [x] 5.3 Include configured projects with no tasks, with an empty state, hide empty groups during search.
- [ ] 5.4 Add terminal recovery, quoted-path and empty-project regressions.

## 6. Optional project reads

- [x] 6.1 Keep task connection and refresh working through non-auth project list failures, with a project warning and the last successful project list.
- [x] 6.2 Preserve project authentication failures and request cancellation guards.
- [ ] 6.3 Add browser regressions for optional project reads and authentication failures.

## 7. Composer attachments

- [x] 7.1 Guard file drops against navigation, highlight composer and agent targets, and preserve non-file drag behavior.
- [x] 7.2 Upload files and pasted images through the existing task upload endpoint, with per-task chips, local previews, removal and explicit retry.
- [x] 7.3 Include ready attachment paths only on explicit send, preserve task isolation and text paste, and clear local resources on disconnect.
- [ ] 7.4 Cover attachment policies and isolated upload/send browser flows.

## 8. Rail archive controls

- [x] 8.1 Add an accessible, vertically centered archive or restore button to every task row without selecting the task.
- [x] 8.2 Reuse existing archive and restore confirmations and effects, naming the task safely.
- [x] 8.3 Retain an archived selection in the main viewer while hiding its row under the current filter, restore its highlight when visible again.
- [ ] 8.4 Add a failing isolated browser regression first, then verify cancellation, keyboard focus, confirmation, restoration and selection cleanup.

## 9. Remembered token

- [x] 9.1 Add checked Remember on this device, saving only after successful connect in a workspace-specific key.
- [x] 9.2 Auto-connect on startup, clear on 401/403, retain temporary failures for retry, and tolerate blocked storage.
- [x] 9.3 Clear credentials on disconnect while retaining remembered tokens on pagehide. Keep drafts memory-only.
- [ ] 9.4 Add failing storage regressions first and isolated browser coverage for refresh, disconnect, rejection and unchecked Remember.
- [x] 9.5 Document credential persistence and verify remembered login in isolated browser flows.

## 10. Integrated frontend verification

- [x] 10.1 Verify integrated attachments, archive controls and remembered login through the isolated browser workflows.

## 11. Rail action appearance

- [x] 11.1 Center the existing small outlined rail icon vertically and preserve the rounded hover highlight without changing the row layout.
- [ ] 11.2 Verify rail action appearance, archive confirmation, remembered login and attachment previews.

## 12. Viewport containment

- [x] 12.1 Reproduce panel placement failures through navigation toggles, agent focus, viewport changes and wide content.
- [x] 12.2 Anchor remaining panels to their intended grid slots and ellipsize long branch/file names without hiding overflow on the page.
- [ ] 12.3 Run isolated viewport and panel containment regressions.

## 13. Nested task rail

- [ ] 13.1 Add a failing browser regression before implementing nested disclosures.
- [x] 13.2 Derive a cycle-safe task forest from existing Hera bindings and bridges without Go changes.
- [x] 13.3 Add recursive project-style disclosures, in-memory collapse state, archive/search handling and needs-input rollup.
- [ ] 13.4 Verify task hierarchy in unit, rail and isolated browser suites.

## 14. Compact task rows

- [ ] 14.1 Add failing browser regressions for one-line task rows, ellipsis, SVG status labels/colors and reduced motion.
- [x] 14.2 Map existing task/PR states and failed Hera role status to compact inline icons while preserving nested input rollup and archive controls.
- [x] 14.3 Document REST status gaps and verify compact rail states.

## 15. General and project settings

- [x] 15.1 Inventory existing web settings and REST endpoints.
- [ ] 15.2 Add failing browser coverage for settings navigation, partial General saves and project CRUD before implementation.
- [x] 15.3 Replace the footer fallback with a gear and main-pane settings shell, preserving task state and history.
- [x] 15.4 Implement General and project management through existing endpoints, preserving returned fields and disclosing API limits.
- [x] 15.5 Run isolated General and project-settings browser suites.

## 16. Conductor composer

- [x] 16.1 Add composer layout, read-only backend/model identity, file picker and regressions while preserving paste/drop/dictation.
- [x] 16.2 Verify composer input and layout in isolated browser workflows.
- [x] 16.3 Use existing REST data for measured context/usage and backend availability.
- [x] 16.4 Cover stale context reads, unknown metrics, accessibility and backend mapping.

## 17. Busy worker icon correction

- [ ] 17.1 Reproduce live in-review workers rendering a PR icon and idle review tasks claiming a nonexistent PR with failing unit regressions.
- [x] 17.2 Derive working activity from live/non-idle session flags across workflow states and require cached PR state for green PR icons.
- [x] 17.3 Add browser coverage for busy in-review rows while preserving terminal PR and needs-input precedence.

## 18. Project rows and task creation

- [ ] 18.1 Add failing browser regressions for empty-project no-disclosure behavior, including an expanded project becoming empty.
- [x] 18.2 Add letter avatars, always-visible project plus, hover/focus actions menu and remove project counts.
- [x] 18.3 Add a main-pane task-creation flow through existing POST /api/tasks with preselected project, native prompt input, optional name, defaults and inline recovery.
- [x] 18.4 Preserve prior view on cancel, guard late results, attach the created task and verify creation against the isolated harness.
- [ ] 18.5 Document task creation scope and API limits in client contracts and gotchas.

## 19. Archive context

- [ ] 19.1 Add browser regressions for selected-task archive with retained filter, scroll and terminal.
- [x] 19.2 Keep archived main selection while filtering its row, and reselect it when restored into the current filter.
- [x] 19.3 Preserve Archived on restore and update existing archive regressions to verify the new context-preserving behavior, including failed task refresh.
- [ ] 19.4 Document archive context retention and verify isolated browser flows.

## 20. Rail task settings

- [x] 20.1 Inventory task REST mutations and safe read-only fields, recording missing targeted operations.
- [ ] 20.2 Add failing browser tests for project Configuration, task menus and captured-task settings/actions.
- [x] 20.3 Reuse Settings' public project-editor entry point, add task menu with keyboard/refresh focus support and task settings routing.
- [x] 20.4 Implement dedicated rename/status/session/account/fork/delete operations with confirmations, inline errors and stale-result guards.
- [x] 20.5 Verify captured-task settings and actions in unit, rail and isolated browser suites.

## 21. Responsive desktop layout

- [ ] 21.1 Add failing wide/ultrawide viewport and terminal geometry regressions before changes.
- [x] 21.2 Fix measured sizing causes and reconcile terminal-fit policy without hidden process restarts.
- [x] 21.3 Retain narrow containment tests and document terminal viewport constraints.

## 22. Automation and System settings

- [ ] 22.1 Add failing schedule policy, metrics formatting and isolated section browser regressions first.
- [x] 22.2 Implement schedule list/create/edit/confirmed delete/run with retained failed edits and one-shot preservation.
- [x] 22.3 Implement visible-only System polling, availability bars, host sleep warning and text-only bounded logs.
- [x] 22.4 Register schedule and System modules through the grouped Settings registry.
- [x] 22.5 Document schedule and System invariants and verify isolated section flows.

## 23. Grouped Settings and remaining parity

- [ ] 23.1 Add failing grouped-nav, repository-targeting, search and mobile-boundary regressions.
- [x] 23.2 Restructure Settings navigation into grouped sections and individual repositories with a section registration contract.
- [x] 23.3 Implement global Sandbox and write-path proposals through existing master-token APIs.
- [x] 23.4 Implement read-only Agents/backends and account visibility.
- [x] 23.5 Implement Notifications/devices, API tokens and isolated-token iOS share URL generation.
- [ ] 23.6 Integrate Schedules and System/Logs sections and document REST gaps.

## 24. Native equivalents

- [x] 24.1 Document native equivalents, intentional differences and named follow-ups in the proposal.
- [x] 24.2 Browser-safe global/terminal shortcuts and help.
- [x] 24.3 Claude session picker.
- [x] 24.4 Add read-only coordinator nesting and dual terminal panes.
- [x] 24.5 Add captured rail actions and pin state.
- [x] 24.6 Complete Changes/Info and creation backend choice.
- [x] 24.7 Connection banner and event-driven updates.
- [x] 24.8 Surgical Ctrl+Z input guard.
- [x] 24.9 Add attention dedupe, burst gate and browser badges.

## 25. Maintenance and Update

- [ ] 25.1 Add failing browser coverage for confirmation/cancel, returned cleanup counts, source-path failures, master gating, build output and stale update/reload cleanup.
- [x] 25.2 Register Maintenance and Update under System through existing REST only.
- [ ] 25.3 Verify Maintenance and Update behavior through isolated fixtures.
- [x] 25.4 Native new-task account selection and actual account-array decoding.
- [x] 25.5 Retained task terminals with independent input cancellation and hoisted freelance detail resolution.
- [x] 25.6 Preserve Task settings as the initial task-menu keyboard entry.
- [x] 25.7 Preserve long-label header containment, physical Alt chords, cache Fit disposal and bounded cursor catchup while releasing hidden streams.

## 26. Terminal scroll ownership and automatic fit

- [ ] 26.1 Add failing single-scrollbar/right-edge regressions at 1280, 1728, 2544 and 3440 widths, plus debounce/hidden/busy/unchanged/in-flight/cache tests.
- [x] 26.2 Reserve actual scrollbar width plus a right gutter, and give vertical scrollback one xterm owner.
- [x] 26.3 Add settled automatic fitting on visible interactive idle sessions, fresh REST busy checks, serialized/coalesced resize and manual fallback.
- [ ] 26.4 Document settled terminal fitting and verify terminal/responsive browser flows.

## 27. Unified rail and explicit split

- [ ] 27.1 Add split browser regressions and cache/policy unit tests.
- [x] 27.2 Replace separate Tasks/Hera modes with compact project task rows and role tags.
- [x] 27.3 Preserve selection, search/archive filtering, pin, actions and attention rollup in the project rail.
- [x] 27.4 Add persistent explicit split toggle, any-other-task picker and compact coordinator roster.
- [x] 27.5 Reuse retained controllers across primary/secondary mounts and closing split.
- [x] 27.6 Add split keyboard chords and help, and document the intentional native divergence.
- [x] 27.7 Remove attention task popover, move persisted alert flags to Notifications and retain burst/dedupe coverage.
- [x] 27.8 Emphasize attention names and collapsed ancestors in both themes, clearing from existing signals.
- [x] 27.9 Run unit, relevant browser and full isolated workflow suites.

## 28. Shared workspace operations

- [x] 28.1 Merge Changes/Files with flat/tree and path copy controls.
- [x] 28.2 Retain task facts in Info, unique session/sandbox facts in settings, trim subtitle.
- [x] 28.3 Share metadata-first PR resolution and selection-scoped links.
- [x] 28.4 Verify daemon lifecycle semantics and consolidate or explain distinct controls.
- [x] 28.5 Remove Projects overview and replace single-item project menu with Configuration gear.
- [x] 28.6 Share confirmation, clipboard, cached rosters and explicit account policies.
- [x] 28.7 Verify Copy worktree path help text.
- [x] 28.8 Verify shared workspace operations in unit and isolated browser suites.
- [x] 28.9 Revalidate project rosters on explicit and periodic task snapshots and cover read counts and failure warnings.
- [x] 28.10 Remove menu workflow transitions, label Pin/Unpin and consume supplied pin state without a selection raw read.
- [x] 28.11 Remove header/menu archive controls, preserve rail, settings and shortcut confirmations.
- [x] 28.12 Consolidate connection Retry in events banner and remove rail refresh, keep session Reconnect.
- [x] 28.13 Use one Desktop alerts Web Push control, remove the duplicate page Notification channel.
- [x] 28.14 Verify only one menu remains and remove dead project-menu state without introducing unused sharing.
- [x] 28.15 Group Switch Claude session beside Stop/Resume for the captured task, preserving Split/Fit.
- [x] 28.16 Verify consolidated controls in unit and isolated browser suites.

## 29. Project rail simplification

- [ ] 29.1 Add failing tests for absent grouping/filter controls and cross-project visibility with an obsolete stored preference.
- [x] 29.2 Remove Orchestration grouping, its preference/rendering/styles and the Hera-managed filter. Keep project nesting, role tags, attention rollup and split behavior.
- [x] 29.3 Record intentional Mac divergences. The Hera sidebar mode and show-Hera-managed filter are not carried over because the nested project rail covers them.
- [ ] 29.4 Verify project nesting and native differences in unit, rail and isolated browser suites.

## 30. Full-window Settings shell

- [ ] 30.1 Add failing full-window, exact task/review restoration, route reload, shared row and 390/1280/2544 containment regressions.
- [x] 30.2 Replace the middle-panel shell with full-window Settings, grouped icon navigation, Back and compact grouped selector.
- [x] 30.3 Apply shared page and row primitives without changing setting behavior or complex content.
- [x] 30.4 Verify unit, Settings browser and isolated workflow suites, strict OpenSpec and whitespace.

## 31. Compact Changes rows

- [ ] 31.1 Add failing coverage for compact rows, directory truncation, exact counts, menus/copy/editor links, keyboard access and flat/tree parity.
- [x] 31.2 Share the existing task-menu popover, use hover/focus chevrons and copyNotice, retain row diff selection and lazy directories.
- [x] 31.3 Add small header view toggle and visible-row exact per-file diff counts, at most four concurrent reads, canceled on task switch, with blank pending/unavailable states and binary markers. Use API summary totals for the header.
- [x] 31.4 Document inherited git comparison-base limits and native differences.
- [x] 31.5 Verify compact Changes rows in unit and isolated browser suites.

## 32. Compact task header

- [ ] 32.1 Add failing compact-height, narrow-overflow, keyboard-label, branch/PR, split and Fit browser checks.
- [x] 32.2 Consolidate task identity and existing action owners into one compact header, retain status/reconnect feedback.
- [ ] 32.3 Verify compact header, responsive geometry, split and terminal behavior.

## 33. Review, menu and split tooltips

- [ ] 33.1 Add failing browser regressions for Changes icons, statuses, exact counts, full paths, task menus and split picker/roster names.
- [x] 33.2 Apply shared data-tooltip attributes and accessible names, removing native titles in the assigned review/menu/split area.
- [x] 33.3 Preserve visible labels for diff/artifact/dialog/shortcut actions without redundant tooltips. Info has no copy controls in this version.
- [ ] 33.4 Verify rendered shared tooltips in review and isolated browser workflows.

## 34. Shared tooltips and rail footer

- [x] 34.1 Add shared tooltip placement and browser lifecycle tests, primitive and documented attribute contract.
- [x] 34.2 Add rail tooltips, live connection icon and single-line footer with reusable icon audit.
- [x] 34.3 Verify shared tooltips and rail footer behavior in unit and isolated browser suites.

## 35. Rail hierarchy geometry

- [ ] 35.1 Add failing 1280/390 light/dark geometry tests for nesting, disclosure slots, icons, selection bounds and status columns.
- [x] 35.2 Use one indent unit, align project actions with SVG glyphs and retain existing rail behavior.
- [ ] 35.3 Verify rail geometry, nesting and tooltips in isolated browser flows.

## 36. Nested-owner correction

- [ ] 36.1 Reproduce the rendered review finding with a failing child-to-parent hover regression.
- [x] 36.2 Compare nearest tooltip owners and cover reverse transitions, same-owner descendants and aria-describedby cleanup.
- [ ] 36.3 Verify nested tooltip ownership in unit and isolated browser flows.

## 37. Composer alignment and control tooltips

- [ ] 37.1 Add failing three-width geometry and control tooltip regressions before implementation.
- [x] 37.2 Normalize action glyphs and targets, keep pending send fixed and add delegated attributes to composer/header/terminal controls.
- [ ] 37.3 Verify shared tooltips and composer geometry in affected and isolated browser suites.
- [x] 37.4 Validate strict OpenSpec and whitespace.

## 38. Attachment submit follow-up

- [x] 38.1 Trace framing and reproduce the early CR with an independent byte log from an isolated fake-agent PTY before implementation.
- [x] 38.2 Add output-settle, bounded retry, acknowledgment and cancellation regressions.
- [x] 38.3 Pass attachment intent through existing send owners and implement attachment-only observed submission.
- [ ] 38.4 Verify attachment submission in unit, composer and terminal browser suites.

## 39. Live Changes and Artifacts

- [x] 39.1 Add deterministic scheduling regressions before implementation.
- [x] 39.2 Refresh selected visible task tools on relevant SSE, focus/visibility and ten-second fallback.
- [x] 39.3 Coalesce reads, abort task switches, retain Changes state and selectively invalidate path counts.
- [x] 39.4 Remove Changes Refresh and offer Retry on failure.
- [ ] 39.5 Verify live Changes and Artifacts in unit and isolated browser suites.

## 40. Informative tooltips

- [x] 40.1 Add rendered regressions for duplicate Settings labels, child-ellipsis task names, untruncated names, icon controls and shortcut-only labeled help.
- [x] 40.2 Suppress normalized duplicate labels in the shared handler, preserve detail tooltips and gate names/paths on truncation.
- [x] 40.3 Remove redundant Settings navigation and labeled copy-button attributes, retaining icon accessible names and overflow-only repository names.
- [x] 40.4 Extend the reusable audit across visible application containers with missing-icon and redundant-label negative controls.
- [ ] 40.5 Verify informative tooltips in unit and isolated browser suites.

## 41. Tighter rail hierarchy

- [x] 41.1 Replace stacked row/disclosure indentation with one 12px step per child container and a 12px overlaid chevron.
- [x] 41.2 Align root task icons with project avatars within 2px and no more than 20px past project chevrons, retaining row, pill and action alignment.
- [x] 41.3 Verify three task levels at 1280/390 in light/dark and preserve nesting clicks at the narrower glyph center.
- [ ] 41.4 Verify hierarchy geometry, tooltips and nesting in isolated browser suites.

## 42. Task disclosure slot

- [x] 42.1 Keep the 12px hierarchy and existing icon/text columns while placing a shared rotating SVG inside the row background with a 4px icon gap and clear accent bar.
- [x] 42.2 Preserve independent pointer/Enter/Space activation and explicit ArrowRight/ArrowLeft expansion without selecting the row.
- [ ] 42.3 Extend geometry assertions for both parent levels, selection states and width/theme combinations.
- [ ] 42.4 Verify disclosures, geometry, nesting and task rows in isolated browser suites.

## 43. Stale ready role tag

- [ ] 43.1 Add failing unit coverage for working plus ready_to_close and for binding selection, then let working outrank ready in `roleStatus` and `taskRoles`.
- [x] 43.2 Extend the unified-rail browser fixture so a working freelance role with ready_to_close renders a working tag.
- [x] 43.3 Check other ready_to_close consumers. Only the role tag and binding selection read it, status icons, attention rollups and failed markers do not.

## 44. Feature folder refactor

- [x] 44.1 Inventory files and importers and approve the feature folder map before moves.
- [x] 44.2 Move modules, styles, docs and tests with git mv and update only path references and recursive test commands.
- [x] 44.3 Verify unchanged unit and browser test cases after feature-folder moves.

## 45. Event and selection audit regression tests

- [x] 45.1 Hold initial and reconnect baselines against controlled SSE frames and assert buffered patches, replay rejection and the reconnect cursor.
- [x] 45.2 Fence late Info raw/links, header links, diff, artifact bytes and manifest reads across task switches and cover missing artifact recovery.
- [x] 45.3 Record API mutations through offline/live task and review switches, allowing only eligible terminal resize and preserving drafts.
- [x] 45.4 Close compact drawers during agent focus and fence held refresh/stream work and scheduled retries after disconnect.

## 46. Settings and task isolation regression tests

- [x] 46.1 Hold cleanup POST, assert disabled cleanup/navigation, duplicate suppression and one task snapshot after completion.
- [x] 46.2 Retain exposed share input across section change, Settings close and disconnect, assert detached input is empty and URL absent.
- [x] 46.3 Mount diagnostics xterm with `?terminal=1`, assert metadata excludes typed sentinel/payload and no API requests (including input).
- [x] 46.4 Destroy a held log read, assert aborted signal, unchanged detached pane/host and unchanged local/session storage. Register dummy section and assert callback identity, baseline retention, busy/status/changed fencing and one destroy per mount.
- [x] 46.5 Fail captured Worker pin while Coordinator has an unsent draft, preserve draft and selection. Successfully create with named Codex account, exact mutation ledger contains creation alone.
- [x] 46.6 Clear collapsed project/header attention classes and computed weight, operate unselected parent disclosure by keyboard while preserving leaf selection and main title.

## 47. Failed binding precedence

- [x] 47.1 Reproduce failed losing to working and ready in both binding orders before fixing the rank.
- [x] 47.2 Rank blocked above failed, then working and ready, following the order in `roleStatus`, cover blocked versus failed in both orders.
- [x] 47.3 Add the competing failed bindings scenario to the unified project task rail requirement.
- [x] 47.4 Preserve the original Argus tab icon and title at zero attention, composite a count badge on the same favicon link, and cover zero/count/reset plus failed and delayed image loading.
