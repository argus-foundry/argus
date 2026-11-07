# Standalone web workspace

## Why

Argus's existing web UI makes task navigation and change review compete with agent output. This optional desktop workspace that makes these views easier to use while retaining the existing agent terminal and API behavior.

## Scope

Serve the new frontend on a separate loopback development port. Its proxy forwards existing authenticated REST and SSE requests to one configured loopback daemon. This preserves response headers such as `X-Output-Total` without changing daemon CORS. The existing web UI remains available and unchanged. Settings and task creation are integrated; unsupported Hera mutations remain outside this client.

The workspace adds project grouped task navigation, independent collapsible rails, agent focus, resizable panels, in-memory per-task drafts, central review tabs, unified and split diffs, and registered artifact previews. It uses existing task lifecycle operations and preserves their consequences.

## Limits

The agent view is the real raw terminal. The workspace does not add conversation rendering, shell tabs, a full repository tree, generic source reads, fabricated context data, or backend guarantees for stale input and git error classification. Visible interactive idle terminals automatically resize after geometry settles for about 400 ms, with fresh REST busy checks, one in-flight resize and coalescing. Hidden/cache-suspended or known busy terminals defer to the explicit Fit fallback. Browser guards reduce task-switching mistakes but do not strengthen backend race guarantees.

Deferred backend contracts are listed under Non-Goals and named follow-ups below.

## Delivery boundary

No Go, daemon, static shell, REST contract, schema or API behavior changes are part of this workspace. Do not restart the daemon. Keep the work local; no push, PR, publishing or deployment is included.

## Non-Goals and named follow-ups

The optional web workspace is the client we converge on. The final desktop-web-workspace specification is the source for a future Mac app rebuild. This change uses existing authenticated REST and SSE contracts and changes no Go, daemon, REST, TUI, original web SPA or macOS behavior. Equivalent capabilities and intentional differences have been evaluated across those surfaces. The following named follow-ups record the remaining gaps without expanding this delivery.

### Client convergence

- `native-workspace-rebuild` rebuilds the Mac client from this change's final specification, covering the unified nested project rail, arbitrary task split, compact header, retained terminals, composer and attachments, measured context, archive context retention, Changes and split diffs, registered artifacts, full Settings breadth, live tools, attention and accessibility. Native credentials, notifications, Finder and window integration remain platform choices.
- `tui-workspace-interaction-parity` evaluates the unified task hierarchy, arbitrary split, per-task draft and attachment composer, measured context, archive viewer retention, resize policy, surgical Ctrl+Z handling, attention rollups, live review updates and corresponding accessible controls for the TUI. Each difference receives an implementation or explicit terminal-specific disposition.
- `legacy-spa-workspace-convergence` decides replacement or selective porting of workspace navigation/history, layout, composer and submission behavior, review controls, retained terminals, attention, settings navigation and accessibility into the original SPA. The original SPA remains available during this transition.
- `workspace-plugin-settings` evaluates existing plugin forms and bridge-budget editing through supported operations.
- `workspace-backend-management` evaluates existing master-only backend CRUD and permission defaults. Agents and accounts remain read-only in this change.
- `workspace-bridge-controls` evaluates existing bridge configuration controls for workspace Settings.

The native `add-web-hera-mutations` follow-up remains separate. Spawn, new coordinator, move, join, detach, inbox and merge-safety Cleanup retain their standing TUI-only disposition. The workspace's rail, split and roster do not add Hera mutations. One unified project rail intentionally replaces separate Tasks/Hera modes, the Hera visibility filter and kanban grouping in this client. Task status remains editable in Settings rather than through quick workflow transitions. Archive/restore remains in the rail. Session selection remains beside Stop/Resume. Web Push is the desktop-alert channel; in-page attention preferences remain separate.

Browser drawers, DOM tooltips and their pixel geometry do not require literal native or terminal copies. Path copy and encoded VS Code links replace native Finder/Xcode launching. Opt-out origin token storage replaces Keychain. Tab title/favicon attention and the unified rail replace Dock/menu-bar affordances. Native window controls remain platform-specific. These equivalents add no backend requirements.

### Backend contracts deferred

The numbered references identify the existing 20-item backend tally. They record scope, not completed fixes.

1. `task-runtime-state-all-statuses` exposes live/idle state for every task with a session. Until then, activity outside in_progress is incomplete.
2. `task-failure-state` exposes task failure independently of workflow status. Until then, only failed Hera roles support a failure indicator.
3. `distinct-pr-terminal-states` separates merged PRs from unmerged closure. The current indicator accurately combines them.
4. `preserve-project-diligence-profile` preserves profile assignment on REST project edits. The current editor warns about the inherited loss.
5. `rest-account-management` defines authenticated account CRUD. Existing account selection does not imply account management.
6. `safe-runtime-model-switch` defines model changes for running tasks. The composer model label remains read-only.
7. `provider-context-usage-contract` exposes scoped context categories, provider identity, Codex quota/account details and per-session/turn telemetry. Unknown fields remain omitted and historical totals stay separate.
8. `agent-prompt-suggestions` exposes verified provider suggestions before adding suggested composer text.
9. `targeted-task-runtime-edits` defines safe backend, model, sandbox and prompt transitions. Generic full-record PUT is not a runtime editor.
10. `cross-backend-account-move` defines account movement beyond Claude. Existing movement remains Claude-only.
11. `durable-task-pr-association` defines manual PR links that coexist with the branch poller. This client reads existing PR links only.
12. `targeted-task-pinning` replaces inherited full-record pin writes with a fenced operation preserving archive invariants.
13. `complete-task-mutation-events` emits events for archive/restore and every task mutation. Reconciliation polling remains required.
14. `multiplexed-terminal-output` provides continuous shared output across visited tasks. Hidden cached terminals currently suspend SSE and recover only the bounded existing replay window.
15. `discard-worktree-file` defines a confirmed per-file discard operation. Changes omits Discard.
16. `git-per-file-numstat` supplies numeric per-file counts. Visible rows currently fetch individual diffs with at most four concurrent reads.
17. `task-base-aware-git-comparison` honors task.base_branch and supplies comparison provenance. Changes currently displays the daemon's whole-stack comparison without client filtering.
18. `worktree-change-events` supplies authoritative worktree updates. Changes and Artifacts retain relevant session-event refresh, focus/visibility refresh and a visible-only ten-second fallback.
19. `clear-stale-role-ready-marker` clears ready_to_close when a role resumes working. Workspace role tags currently give working precedence over the stale marker.

20. `codex-recapture-test-env-isolation` isolates CODEX_HOME and CODEX_SQLITE_HOME in the daemon recapture fixture. This is a pre-existing test issue on master, not a product defect.

The broader existing backend exclusions also have names. `conditional-pty-input` checks input against process identity at the PTY owner. `identified-git-outcomes` defines comparison identity and explicit missing-worktree, git-failure and stale-snapshot outcomes. `task-shell-and-source-browsing` defines independent task shells and bounded repository content reads. `provider-conversation-feeds` defines verified provider conversation rendering. `durable-review-feedback` ties feedback to comparison identity and external review synchronization. None is implemented here. Client request fencing, bounded attachment submission and settled terminal fitting do not strengthen these backend guarantees.

## Rail archive controls

Each rail task offers a right-aligned, vertically centered archive button on hover or keyboard focus, with a restore button in Archive. Both use the existing confirmation and lifecycle operation, with the task name rendered as text. Archiving the selected task retains its main viewer and the current rail filter and scroll, while its filtered-out row has no highlight. Restoring reselects the row whenever it is included in the current filter. This is confined to the optional frontend; existing TUI, static web and macOS behavior and the REST contract stay unchanged.

### Rail abstraction inventory

| Name | Type | Boundary and relationship |
| --- | --- | --- |
| Task item | Visual component | Contains sibling task selection and archive action buttons. |
| Rail archive action | Operation | Opens TaskArchive for its row task without changing selection. |
| TaskArchive | Operation | Owns the existing confirmation, consequence explanation and lifecycle request. |
| Archive selection | Policy | Retains the main selection and viewer; the current rail filter determines its visible highlight. |

Existing spacing, color and focus tokens style the row action. No new backend abstraction is needed.

## Remembered token

Login offers Remember on this device, checked by default, using only the workspace origin's `argus-workspace-token` localStorage entry after successful connection. Refresh reconnects automatically. Disconnect and authentication rejection remove it; temporary errors preserve it for retry. This matches the original loopback UI's security posture and changes only credential persistence. Drafts stay in memory, and blocked storage falls back to memory-only operation.

## Settings

A footer gear opens full-window Settings with General and project management through existing REST endpoints. Settings covers General, repositories, Sandbox/proposals, read-only agents/accounts, schedules, notifications/devices, System/Logs, Maintenance and Update through existing REST. Request limits and project-editor routing are recorded in `design.md`. The daemon contract remains unchanged.

## Nested task rail

Nest bound workers beneath their coordinator using the existing Hera REST roster, including multiple sub-coordinator levels. Parent disclosures match project groups, retain state in tab memory and signal hidden descendants needing input. Archive and search continue to determine which tasks appear. No REST or other frontend changes are included.

## Project task creation

Project rows gain letter avatars, always-visible task creation and a small supported-actions menu. Empty projects have no disclosure. A minimal main-pane form creates a task through the existing API with the project preselected, a prompt and optional name. Repository management is available in Settings through the existing project APIs. Backend changes remain outside scope.

## Composer parity

The composer follows the Conductor layout, with a large rounded draft box without placeholder text and a bottom bar containing a read-only backend/model label, context ring, file picker and send arrow. Chips, file drops, image paste and dictation remain available without a persistent attachment instruction banner.

The context popover uses existing authenticated REST reads only. Show measured fields with accurate scope, omit unsupported values, and distinguish missing context from zero. No Go or other frontend changes are included; parity with the existing TUI, static web and macOS composer is covered by the named client convergence follow-ups above.

| Abstraction | Responsibility |
| --- | --- |
| TaskComposer | Draft, native paste and explicit send lifecycle. |
| ComposerAttachments | File picker, drop/paste uploads and attachment chips. |
| Composer identity | Captured task raw metadata, read-only backend/model display. |
| Composer context | Read-only measurements and accessible popover, isolated from draft lifecycle. |
| Context policy | REST field mapping, scope and unknown-value rules. |

## Responsive desktop

The layout supports 1280, 1920, 2560 and 3440 CSS pixel displays and the 320px drawer layout. Columns must stay within the viewport, rails retain bounded widths, and the center/composer consume the remaining space. The terminal viewport must track its available pane rather than a max-content wrapper. Settled visible idle terminals fit automatically under the guarded resize policy. Explicit Fit remains the fallback for deferred fitting.

| Abstraction | Responsibility |
| --- | --- |
| Workspace columns | Bounded navigation/tools tracks and a fluid remaining center track. |
| Terminal viewport | Available client content width and height, separate from authoritative PTY cells. |
| Terminal geometry | Fits client cells and submits PTY changes only under the guarded resize policy. |
| Responsive browser evidence | Viewport/column bounds and terminal geometry after window and panel changes. |

## Automation and System settings

Settings provides REST-only sections for schedule management, host metrics and bounded logs through the grouped section registry. Constructors take `{api, host, status, changed, busy, baseline}` and synchronous `destroy()` cancels section resources. The Settings shell owns navigation and registry integration; these modules do not own task or rail navigation.

| Abstraction | Responsibility |
| --- | --- |
| ScheduleSettings | Schedule list and explicit CRUD/run operations, captured row identity and deletion confirmation. |
| Schedule editor | Editable schedule fields, backend model suggestions and recurring/one-shot cadence. |
| Schedule policy | Partial request payloads that preserve unchanged one-shot times and validate required input/body size. |
| SystemSettings | Available host measurements, risk warning and polling scoped to visible section/tab. |
| LogSettings | Bounded plain-text log selection/refresh with cancellation and local scrolling. |

No metrics or commands are invented. Host sleep commands are displayed as text and never executed. Only the existing endpoints are used; no daemon, other frontend or schema change is required.
