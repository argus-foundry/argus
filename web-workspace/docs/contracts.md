# Additive web client boundaries

Theia uses existing authenticated REST and SSE APIs with no Go, static shell, database or daemon behavior changes.

A footer gear opens full-window Settings with General, repositories, Sandbox/proposals, read-only agents/accounts, schedules, notifications/devices, System/Logs, Maintenance and Update through existing REST APIs. Settings coverage and request limits are recorded in the archived [proposal](../../openspec/changes/archive/2026-11-07-add-desktop-web-workspace/proposal.md) and [design](../../openspec/changes/archive/2026-11-07-add-desktop-web-workspace/design.md).

The frontend is served on a separate loopback port by `dev-server.mjs`. Its development proxy forwards existing `/api/` requests unchanged to one configured loopback daemon. This preserves response headers such as X-Output-Total without changing daemon CORS. `/vendor/` serves the existing checked-in xterm assets read only. No dependencies or build are needed for the client.

## Module interfaces

`core/api.js` exports `Api` and `log`. Construct `new Api()` and call `setToken(token)` with the active token held in memory. Credential persistence follows Remembered token below. `request(path, options)` returns a checked fetch Response and adds Authorization. `json(path, options)` parses JSON. `streamUrl(path)` adds the current token for EventSource, preserving query parameters. Paths are existing relative `/api/` paths only. Request options support AbortSignal. Do not log tokens, request bodies, URLs containing tokens or conversation output.

`terminal/terminal.js` exports `AgentTerminal`. Construct `new AgentTerminal(api, container, onState)` where `onState` receives `{message, kind, interactive}`. `attach(task)` asynchronously selects and replays a task. `show(visible)` changes view visibility without stopping an agent. `fitSession()` deliberately resizes the selected live PTY to the visible pane through the existing resize endpoint. `send(text, task)` explicitly targets the supplied task and returns a promise. `destroy()` detaches browser resources only. Fetch authoritative size first, then automatically fit visible interactive idle terminals after 400 ms of settled pane geometry. Fresh REST task state and visibility are checked immediately before resize, with serialized/coalesced requests. Hidden/cache-suspended, recorded and known-busy sessions keep their authoritative size; manual Fit can override the busy deferral. Preserve Ctrl+Z protection, generation guards, output snapshot cursor to SSE continuity, bounded reconnect, history reading, and cleanup. Existing backend replacement races remain an explicit limit.

`workspace/app.js` and view modules own DOM shell, task navigation, per-task in-memory drafts, selected review location, async cancellation and panels. Backend output is always the real agent terminal, not simulated conversation. Tools cover existing Changes, changed/untracked paths, registered Artifacts and task Status. Unified/split diffs use returned diff text; no full tree, source reads, fake context metrics, independent shell or automatic review feedback. Settings uses the sections documented below. Unsupported Hera mutations retain their named follow-up disposition in the base specification. Archive/restore uses existing endpoints with clear consequences and confirmation. Do not auto-invoke mutations.

`TaskComposer(send, api)` owns native text input and explicit submission. Its `ComposerAttachments` child renders per-task chips and guards window file drags, accepting drops only on the composer or agent panel and images pasted into the composer. Other editable controls retain paste behavior. Plaintext paste retains its existing explicit insertion path. Text-only drags remain native.

`AttachmentStore` holds local files, image object URLs, upload state and confirmed paths per captured task ID. Each chip uses existing `POST /api/tasks/{id}/upload` with one `files` FormData part and no explicit Content-Type. A draft is capped at 20 files, 100 MiB each and 250 MiB total. The 280 second timeout stays below the existing server read timeout. Failed uploads require explicit retry or removal; aborts are unconfirmed and may have saved a file. Switching tasks leaves the original upload with its task. Disconnect/pagehide abort uploads, clear timers and revoke preview URLs; removed late results are ignored.

Upload never sends terminal input. Explicit Send appends confirmed paths to text, separated by spaces, and may send paths alone. Pending/error chips block submission, and removals are disabled during an in-flight send. Successful sends consume only their captured chips; newer chips and edited drafts survive. Removing a chip only removes its message reference and local preview, not an uploaded server file. Attachment contents, names and paths never enter browser storage or client logs.

No frontend framework, private data fixtures in production, package publishing, external fonts/CDNs, real daemon testing, remote branches, pushes or PRs. The push service worker handles notifications and clicks without intercepting fetches. All names/text from APIs must be rendered safely. Artifact HTML must be sandboxed and script disabled; no daemon token in iframe content. Geometry preference may use localStorage. Only credentials and geometry preferences may be persisted as described below; draft text may not be persisted. Reconcile OpenSpec before implementation is declared complete.

## Remembered token

`core/token-storage.js` exports `TokenStorage` and `TOKEN_KEY` (`argus-workspace-token`). It owns all credential localStorage access behind try/catch, including access to the storage property itself. `read()` supplies startup auto-connect; `connected(token, remember)` stores only after successful connection or clears the saved entry for memory-only use. `rejected(error)` clears only on 401 or 403. `clear()` handles explicit disconnect. Network, timeout and 5xx failures retain the saved token for Retry connection without exposing it in the password field.

Remember on this device is checked by default, matching the original UI's localStorage security posture on a loopback-only client. Storage is scoped to this workspace origin and key; the original UI's token is never read. Blocked storage falls back to memory-only connection. Disconnect clears saved and in-memory credentials and drafts. Pagehide disposes viewers and clears in-memory credentials and drafts without removing a remembered token. Drafts are never persisted. Tokens are never logged or placed in URLs except the existing EventSource `streamUrl` mechanism and explicitly generated credential-bearing share links.

## Settings workspace

`WorkspaceConnection` owns the existing login, remembered token and connection lifetime; `workspace/app.js` keeps task selection and view coordination. `WorkspaceSettings` owns the footer gear, section routing in `?settings=`, section cancellation and return through Escape, Back or task selection. It hides and inerts task controls while leaving the terminal attached, and releases form data on disconnect/pagehide. Request generations prevent old settings callbacks from changing a new view.

General PUT sends only KB enabled/vault path, default backend/share project and API enabled. API port is read-only. Vault/API changes show restart-required information without restarting anything. Project POST checks current names because the API otherwise upserts; PUT preserves the full returned project shape and undisplayed Apple Events overrides. The 4 KiB request limit is checked locally. Remove confirms that tasks/worktrees remain, and successful writes refresh the project list and task rail. A failed write keeps the form and reports an unconfirmed save.

Project API replacement clears diligence profiles that the API never exposes. The editor explicitly discloses this existing limitation; it does not invent a profile field or claim lossless updates. No live daemon or real project configuration is changed by automated tests.

Task rail snapshots are invalidated by task.updated and specific task events, coalesced for 100 ms. The visible 30 second task fallback stops after the first task.updated and restarts on stream reconnect until another such event. Reconnect, visibility return and Retry connection recover missing persisted events. Pin uses POST /api/tasks/{id}/pin with only pinned, and applies returned flags including restoration.

## Task hierarchy

`workspace/rail/task-hierarchy.js` reads the existing `/api/hera` roster alongside task snapshots under the same abort and generation guards. Live worker bindings attach to their coordinator task; orchestrator bridge role IDs attach distinct sub-coordinator tasks. Shared coordinator tasks appear once. The first parent in orchestrator ID order wins for multiple bindings; absent parents, detached bindings and cycle-producing links stay roots. Freelance roles stay independent. Non-auth roster failures fall back to flat navigation with a warning rather than discarding tasks.

`TaskNavigation` owns parent disclosure preferences in tab memory, like project collapse. Native summary controls accept Enter and Space, with separate selection and archive buttons. Descendants follow their root's project even across projects; filtering out a parent promotes its matching children to roots. Search opens matching families without clearing preferences, history reveals ancestors, and disconnect or reload resets disclosure state. Non-archived task needs-input and blocked/needs-input role flags roll up through the visible forest.

## Compact task status

`workspace/rail/task-status-icon.js` maps existing task, cached PR and Hera role signals to labeled inline SVGs. Attention takes precedence over failure, completion, active work and PR status. Failed Hera roles provide a failure signal; ordinary task REST payloads have no failure field. REST combines merged and closed PRs as `merged-closed`, so that purple icon is labeled PR merged or closed. Unknown PR cache states imply no review claim. Active non-idle sessions spin unless reduced motion is requested. Rows contain only the icon and one truncated task name; the existing sibling archive button remains centered.

## Composer layout

`ComposerIdentity` reads only the captured task's existing `/raw` endpoint for backend/model identity, cancels on selection and ignores late responses. Model stays read-only because there is no dedicated runtime model-switch operation. The generic raw-task overwrite is not a safe model picker contract.

The rounded composer keeps the draft above its action bar. Attach opens a native multiple-file picker through `ComposerAttachments.add`, sharing upload constraints, task capture and errors with drag/drop and paste. Persistent attachment instructions are removed; chips and actionable errors remain. The context control displays available measurements through ComposerContext.
A task's workflow status does not establish activity or a PR. Busy live sessions spin even in review/complete workflow states; idle in-review tasks without a cached PR remain gray. Combined terminal PR state, input attention and failures retain their precedence.

## Project creation

`workspace/rail/project-group.js` renders supported project actions, native disclosure only for non-empty projects, letter avatars and a Configuration gear without duplicate creation controls. `NewTaskView` owns a main-pane form, preselected project, optional name, native prompt editing and per-project in-memory creation drafts. It posts existing JSON fields to `/api/tasks`, leaving backend/branch defaults to the daemon, then reads the created task and selects its agent. Errors retain typed fields; cancel/disconnect aborts requests and fences late results. Canceled requests may have created a task and require checking the refreshed rail before retrying. Creation drafts clear on disconnect/pagehide. Attachments remain in the existing task composer after creation; no pre-creation attachment control is included.

Task creation and repository configuration, addition and removal use existing APIs. Settings navigation owns repository management and the project rail gear opens its editor directly. Repository hiding and create-from menu actions are omitted.

## Composer context

`ComposerContext` owns only read requests and a native auto popover. On task selection it reads bridge state, Hera metadata, account readings, backend kinds and the roster, then refreshes every 15 seconds and on open. Results are applied behind abort and captured-task guards; disconnect cancels reads and the timer, clears readings and closes the popover. The identity child supplies its captured raw task for account matching.

`contextSnapshot` maps measured fields into context, account windows, identity and role-history totals. Quota never drives the context ring. Only a live bridge may supply live context. Persisted stamps show record time and do not infer a limit; different stamp timestamps cannot be paired. Use backend roster kind to classify custom backend names. Shared-account Claude identity/usage never appears as Codex identity/usage. See `docs/context-data.md` for per-backend availability and needed daemon work.

## Archive context

Archive/restore retain the main task, viewer, rail filter and scroll position. A filtered-out task has no rail highlight; restoring reselects it when the current filter contains it. Keep the local archived flag and header/input state current even if the subsequent task read fails.

## Rail task settings

`TaskNavigation` routes Configuration to the public `WorkspaceSettings.openProject` entry point and task menus to `TaskSettingsView`; row selection remains independent. `workspace/rail/task-menu.js` owns the native popover and captured copy/pin/settings targets, including focus restoration through rail refresh. `workspace/workspace-route.js` dispatches history locations between task, project/general settings and task settings without replacing the current main task.

`TaskSettingsView` reads the captured task DTO/raw record plus optional account/backend/PR metadata, fences results with abort and generation guards, and uses only dedicated mutation endpoints. Forms and confirmed actions are children, with failed edits retained and no raw-task overwrite. Delete clears a selected viewer locally before a possibly failing task refresh. Account movement appears only for a roster-verified Claude backend and another compatible account, never silently forcing activity. Existing archive confirmations and filter/scroll retention apply from settings too. PR links accept only HTTP(S). Copy tests stub the browser clipboard rather than touching the OS clipboard.

Sandbox/runtime model/backend/prompt changes, base-branch changes and durable PR association editing are not inferred from generic raw or metadata writes. See the task settings inventory in the archived OpenSpec change for endpoint coverage and remaining daemon operations.

## Responsive terminal geometry

`TerminalGeometry` observes client pane and cell-renderer dimensions, reports a valid proposed-cell mismatch and disposes its observers/animation frame on detach. Its observations never mutate xterm's authoritative cells or the backend PTY. The client host fills the pane content box; wider/taller existing terminal cells may overflow only in the terminal viewer.

The existing Fit session button gets a subtle highlighted state and a tooltip with proposed/current dimensions when they differ. Fit measures visible host client width and vendored xterm cell metrics, reserving at least 16 px for native/overlay scrollbar space, a 4 px right gutter and a small column-scaled glyph overhang allowance. Window, drawer, rail, focus and renderer changes schedule settled fitting; the hint is shown only when automatic fitting is skipped or fails. Recorded sessions can show the mismatch but Fit stays disabled until the task is interactive.

Xterm's root inherits its configured cell font explicitly so screen-reader rows and renderer rows have the same character widths. Transparent accessibility text must not use the workspace's proportional body font, which expands long lines beyond correctly fitted cells. Screen-reader support remains enabled.

### Settings section registration

`settings/settings-sections.js` exports ordered `settingsSections` descriptors with `{id, label, group, keywords?, create(context)}`. `create` receives `{api, host, status(text,error=false), changed(), busy(boolean), baseline(initial)}` and returns a view with `destroy()`. Section modules own AbortControllers and release resources on destroy; callbacks are generation guarded by WorkspaceSettings. `busy(true)` disables Back and navigation during a mutation. `changed()` refreshes repository navigation and task metadata. No section accesses credentials directly.

The independent modules `schedules` in Automation and `system`/`logs` in System are registered through descriptor factories. `openProject(name,{history})` opens `repository:<name>` through the same lifecycle, and `add-repository` opens an empty form. Repository navigation is the only project list; editors use `repository:<name>` routes. Search filters nav entries by label/keywords and repository name/path, with hidden empty groups; mobile nav is bounded and scrolls independently.

## Automation and System sections

`ScheduleSettings`, `SystemSettings` and `LogSettings` use the grouped registry's object context and own only their supplied host. Schedule mutations capture a saved row ID, disable section/nav controls while pending and never retry automatically. Delete confirms that existing tasks are unaffected. Run now uses saved fields; failed edits remain in their form. Partial updates omit unchanged cadence, preserving past one-shot timestamps and their precision. Cron syntax remains validated by the daemon, not a guessed client parser.

System polling follows the existing 2 second cadence only while its section and browser tab are visible. Each read finishes before another poll is scheduled. Availability flags and valid values control bars; absent measurements are not fabricated zeros. The host-sleep risk message and suggested permanent command come from REST; the existing temporary caffeinate command is displayed as text. No command is executed.

Logs read only the existing UX/daemon 64 KiB tails, render via textContent and scroll inside a focusable monospace panel. Selection/refresh abort superseded reads, and destruction ignores late text after switching sections. Logs, metrics and schedule drafts are never persisted in browser storage or copied into client logs.
Sandbox sends only `sandbox.enabled`, `deny_read` and `extra_write`; unedited Apple Events grants remain server-owned. Global saves and proposal actions require a master token. Proposal drafts survive failed decisions, approval explicitly confirms exact paths/scope and warnings, and all Sandbox mutation controls are disabled together to prevent overlapping grants and stale form writes. Changes apply to new sessions only. Agents displays existing backends/accounts read-only; account failure does not hide backend data. No account creation or host sign-in API exists.

Notifications & devices uses a workspace-local push-only worker without fetch interception or caches. Browser permission and worker registration happen only after Enable. Disable unsubscribes this browser; deleting a registered server device is a separate confirmed action because the list returns masked endpoints, preventing reliable endpoint matching. Test push explicitly confirms delivery to all registered devices. Actual external push delivery remains a manual check; isolated tests mock subscription transport and exercise real local worker activation separately.

API token management requires master. Minted plaintext and generated share URLs stay only in the current section DOM and are cleared on hide/close/disconnect. Clipboard copy is explicit. The iOS helper requires the original Argus origin reachable by the sharing device, validates HTTPS or HTTP loopback, and builds the existing `/share` flow with the connected credential only after explicit Generate. It does not turn this optional frontend into a share-target implementation or tunnel.

Maintenance and Update are System sections. Cleanup confirms the full completed-task/session-log/worktree/branch/orphan deletion boundary, reports server counts including Hera-bound skips, and refreshes task metadata after success. Cleanup and update never retry automatically. Source-path GET/PUT and update require master; the source request limit is 4 KiB. Update requires a nonempty saved path matching the displayed input because POST /api/update reads server configuration and accepts no path body. It shows build output via textContent, explicitly opts into raw error responses for that endpoint, and keeps default API errors sanitized. A confirmed restart request schedules the original UI's five-second reload, with the section's mutation controls locked until reload or closure; destroy cancels that timer. Tests stub both prune and update, never invoking a real build, restart or bulk deletion.

## Native desktop parity

Native equivalents and named follow-ups are recorded in the [base specification](../../openspec/specs/desktop-web-workspace/spec.md#non-goals-and-named-follow-ups). Shortcuts use an explicit terminal allowlist. TaskNavigation owns one compact project rail with coordinator nesting. SplitWorkspace owns the separate, persistent split toggle, secondary task picker and compact coordinator roster. Events subscribe before baseline reads and buffer through snapshots, with reconnect, visibility return and Retry connection recovery for missing persisted events. Browser attention owns per-task dedupe, a distinct-task rolling burst window and title/favicon counts. The native new-task account choice resolves project/backend support before creation.

`TaskTerminalCache` retains a controller and its DOM view for each visited task. Selection cancels that controller's pending input and suspends its output stream, then mounts the selected view. Foreground catchup appends only unseen bytes using total minus snapshot byte length as the snapshot's start cursor; a counter reset, replay gap or changed geometry replaces the bounded replay instead. Only missing snapshot task IDs and disconnect/pagehide dispose cached controllers. Hera resolves hoisted freelance roles through their owning orchestrator ID.


`terminal/terminal-autofit.js` owns the 400 ms debounce and one in-flight operation for both manual and automatic fitting. `terminal/terminal-resize.js` retains the existing rerender recovery path, adds fresh task preflight for automatic requests and authoritative-size checks for all requests, and checks the controller generation/debounce version/visibility after those reads. An already-matching server size updates the cached view without posting; a newer automatic geometry target is deferred until its own settle window. Busy means existing REST `live:true` without idle/needs_input; absent legacy live state falls back to debouncing. Cached show/resume and task metadata updates re-evaluate eligibility; hidden/document-background transitions cancel timers. `terminal/terminal-geometry.js` keeps the host's visible client space separate from an enlarged authoritative canvas so the last column remains reachable before fitting. Only xterm owns vertical scrollback.

Historical accessibility rows may retain their original wider text after PTY shrinking. Paint containment applies only to the transparent accessibility overlay; the full historical text remains present in Chromium's accessibility tree, with rendering and actual scrollback unaffected.

xterm synchronizes its hidden input anchor on cursor movement, so Geometry also re-synchronizes that vendored input position after resizing without taking focus. It parks offscreen history-reading anchors and leaves active composition untouched.

The vendored DOM renderer can accumulate subpixel text spacing beyond its logical canvas width. Renderer rows receive the reserved glyph paint allowance without changing column positions, clipping visible characters or disabling accessibility. Fit calculation uses device-cell metrics normalized by DPR rather than rounded canvas-width/column ratios, preventing one-column feedback resizes after fitting.

## Unified rail

The project rail uses `taskForest` and `projectGroup` exclusively. `workspace/rail/task-grouping.js` supplies live role tags; `workspace/rail/task-row.js` renders compact rows. No grouping control or Hera-managed filter is present. Stored `rail-grouping` is ignored without migration, while `split-view` remains independent. The Mac Hera sidebar and show-Hera-managed filter intentionally yield to nested project navigation.

`TaskTerminalCache` has primary and secondary mounts. A controller can be mounted once; selecting the secondary task as primary releases its old slot first. Closing split pauses the secondary input/output and detaches its view without deleting the cached controller. State and Fit callbacks route through the current slot, so a secondary terminal cannot change the main composer. A shared visibility-scoped roster refresh serves role tags, nesting and split, with no Hera mutations.

`workspace/attention/attention-preferences.js` shares the existing device-local alert flags between Settings Notifications and the active alert controller. Browser permission requests occur only on explicit enable and late results are fenced after leaving Settings. The unified rail is the sole task list; next-attention uses its filters. Attention emphasis derives from task/role signals without read state.

## Shared workspace operations

`Api.json` coalesces backend and full-project roster reads and task output-link reads. Each caller has its own cancellation signal; canceling one consumer cannot cancel another. Errors evict only their own pending entry. Explicit project Refresh, successful settings/project/backend writes and token changes invalidate cached reads; task reselection invalidates output-link entries. Cached values are cloned per consumer. This cache is scoped to one credential lifetime and never browser storage.

`resolvePR` accepts only credential-free HTTP(S), prefers `pr.url` metadata and then output links marked `isPR`. Header, settings and shortcut share that policy. Changes renders a single git-status path list in flat/tree mode, retaining lazy untracked-directory expansion; both presentations share path-copy feedback. Info reads raw identity fields absent from the task DTO and fences late results.

`confirmDialog` owns explanations with optional structured task details and danger styling, initial Cancel focus, Escape, focus restoration, abort cleanup and asynchronous mutation failure feedback. Promise-only confirmations are canceled by section destruction; callback confirmations retain the dialog on failure. `copyNotice` owns clipboard success/failure feedback, with credential-specific status and abort guards. Account creation allows compatible Claude/Codex accounts plus default; movement requires an authoritative Claude backend and another Claude-compatible account.

`readProjects` sets `revalidate:true` for rail snapshots, including periodic and explicit refresh. `Api.json` then replaces a settled cache entry while sharing one still in flight. This option is client-only and never forwarded to fetch. Optional project failures retain navigation's last project list with its warning instead of replaying a cached success.

## Task control ownership

`workspace/rail/task-menu.js` owns the task menu with task settings, pin and copy entries. `RailActions` tracks pin knowledge separately from membership; present boolean snapshot fields reconcile pin state on load, reconnect, resync and fallback polls, absent fields retain cached state, and reads started before newer local or SSE observations cannot regress them. Pin invalidations read the single-task DTO only for changed_fields containing pinned. Selection and menus issue no extra pin reads, and mutation posts only the desired pinned flag to the targeted pin endpoint. Task settings remains the workflow status editor. Rail icons, task settings and the keyboard shortcut remain archive entry points; the header has no archive action.

Task settings owns the session-action row, including a roster-supported Switch Claude session control. `ClaudeSessions.open(task)` captures the supplied task independently of the selected main task; leaving settings or switching tasks cancels its picker. Successful switching refreshes the corresponding task facts and only reconnects the main terminal if it is that task. Split, Fit and terminal Reconnect retain their existing owners.

`BrowserPushSettings` owns Desktop alerts permission/subscription. `WorkspaceAttention` owns only in-page alerts, burst/dedupe and tab badges; it never constructs Notification objects. Stored banner preferences are ignored. Needs-input and idle preferences affect in-page alerts, not the daemon's push subscription. External delivery remains a manual check; fixture tests do not send real pushes.

The events banner owns connection Retry and snapshot recovery. Footer status contains no retry instructions and snapshot refresh belongs to the events banner. Data-specific stale-project/roster warnings remain explanatory; their failures also expose the banner's Retry. Session Reconnect remains local to terminal output recovery.


## Full-window Settings mode

`WorkspaceSettings` owns the whole window while active, including the shared page title and bounded content frame. The task rail, tools, separators and workspace header are hidden and inert, with prior inert values restored on dismissal. Existing settings routes retain task/view/review selection; Back, Escape and browser history restore that context and in-memory drafts. Reloading a settings route opens the same full-window mode.

`SettingsNavigation` owns the Back control, desktop search with `/` focus shortcut, icon entries and grouped mobile selector. At 700 px and below only the Back/selector bar is shown. Both navigation presentations use the same registry and busy state. Shared `field`/`select` rows retain label/hint accessibility and form semantics, with left descriptions, right controls and separators. Complex content stays inside the page frame with its existing lifecycle.

## Compact Changes

TaskChanges owns flat/tree presentation, lazy directory reads and header totals parsed from BranchDiff/Diff summary lines. ChangedFileCounts uses IntersectionObserver-visible rows only, at most four concurrent existing git/diff reads, a per-task/path-summary cache and cancellation on task/view selection changes. Pending/unavailable counts stay blank and binary diffs show bin. Scaled stat bars are never a per-file count source. The per-file numbers describe the endpoint's returned diff, which prefers working changes before its branch fallback.

ChangedFileRow shares rowMenu with taskMenu. Native popovers retain keyboard arrows, Home/End, Escape and captured paths; copyNotice owns clipboard feedback. The editor action uses only an encoded vscode://file path under a known absolute worktree. No generic custom-protocol API links, discard action or backend changes are added. Directory prefixes alone truncate; exceptionally long filenames scroll locally. The list still deduplicates the exact returned BranchFiles/Status paths and does not choose a comparison base.

## Compact task header

`WorkspaceTaskView` supplies the task name and full-name/project tooltip. `TaskHeaderLinks` keeps captured branch copy and safe PR resolution. The header contains the existing roving Agent/Review tablist and a separate labeled terminal-action group; `SplitWorkspace` still owns the split toggle, while the app and terminal cache retain Fit ownership. Normal terminal status is visually clipped in its live region; warning/error feedback exposes Reconnect above the terminal. Header styles are isolated in `tasks/task-header.css`.

## Composer controls and shared help

The footer owns one centered row with a 32px identity line and three 32px targets. Context, attachment, send and pending-send glyphs use 16px geometry. The composer focus border replaces the textarea's separate square focus outline. The model identity remains read-only. Tooltip text uses the shared delegated data-tooltip contract alongside aria-label; controls carry no native title. Context help follows the current measured/unavailable snapshot, Split includes its shortcut and open/close state, and Fit retains proposed/current dimensions. Stop, Resume and Switch Claude session keep their existing settings owner and explain the existing operations.

## Shared tooltips

`shared/tooltip.js` exports `installTooltips()`, installed once by app.js. One delegated handler per event type supports dynamic `[data-tooltip]` owners without per-control listeners. Icon-only controls provide `data-tooltip="Settings"` and a separate `aria-label="Settings"`; labeled controls use their text as their accessible name. Optional `data-tooltip-shortcut="⌥⇧A"` adds a dim key hint. Remove native `title` from each opted-in control. Labels and shortcuts render as text only.

The single reused `role=tooltip` bubble appears after 400 ms hover or immediately on focus-visible, flips below when needed and clamps to the viewport. It hides on pointer leave, blur, Escape, scroll, resize and click. While visible its ID is added to aria-describedby without replacing existing IDs, then removed on hide. Tokens follow workspace themes, reduced motion disables its fade, and an open modal owns the bubble in its top layer. `installTooltips` is idempotent and returns a disposer for isolated tests.

The rail footer uses `connectionState(message, state)` for disconnected, connecting, connected and error indicators, updating data-tooltip and aria-label atomically. WorkspaceConnection owns connect/disconnect transitions; the existing snapshot refresh owns read-failure/recovery transitions. Shortcut help stays owned by WorkspaceShortcuts and is inserted before the existing Settings gear. Rail icon coverage can be reused as `auditIconTooltips(page, containerSelector)` from tests/icon-tooltip-audit.mjs.

Nested data-tooltip owners are supported. Pointer transitions compare the nearest owner of both event targets, so leaving a count/status tooltip for its parent row shows the parent tooltip while movement within one owner's plain descendants retains the current bubble.

Tooltips appear only when they add information. Icon-only controls describe their action; truncated names/paths reveal the full value only while clipped; other tooltips explain shortcuts, counts or status beyond a label. Never repeat an untruncated visible label. The handler normalizes whitespace and checks the owner plus ellipsis-bearing text children before showing. Name/path owners use `data-tooltip-overflow` to require actual truncation, including when badges or a shortened basename change the owner's text. On labeled controls a shortcut hint is shown alone.

Native select option clipping does not appear in scrollWidth. Full-value select tooltips measure the selected text in the control's font against its padded width and disclosure arrow.

## Live task tools

ToolRefresh schedules selected-task Changes and Artifacts reads on session.idle, session.needs_input, session.exited, task.status_changed and resync; tool/pane visibility return and window focus also refresh. Its ten-second fallback only reads while the pane and focused document are visible. Requests are single-flight, bursts coalesce into one follow-up and task/tool switches abort and fence old results. The daemon has no git-change event, so a future daemon git-change event would remove the fallback poll.

TaskChanges fingerprints decoded paths, statuses, per-path stat lines and summary totals. Identical results leave DOM untouched. Updates retain mode, scroll, tree and lazy-directory expansion, row menus and the independent opened review. ChangedFileCounts retains unaffected paths, invalidates changed status/stat lines and fences older per-path reads while preserving its four-read cap. The existing scaled stat lines are change indicators only, never numeric per-file counts. Changes has no Refresh control, says No changes yet when empty, and fetch failure offers Retry without discarding the last successful view.

## Attachment send acceptance

TaskComposer forwards attachment intent through TaskTerminalCache and AgentTerminal to TerminalInput. Attachment submission writes one closed paste, then waits for fresh output and 100ms quiet outside Claude's transient Pasting… status. That wait is bounded at five seconds, including absent output or unknown layouts. Literal image paths are valid settled input. A final live check precedes exactly one standalone CR, serialized with other terminal input.

The daemon's successful input response resolves the send and clears the captured draft and attachment chips. Screen contents are not a submission acknowledgment and never justify a retry. Cancellation or failed input requests retain the draft with the existing check-before-resending message. Explicit disconnect keeps its clearing behavior. Plain text, other frontends and the REST contract remain unchanged. Input text is never logged.

`TaskClipboard` owns the selected task’s staged text. Selection and output-stream connection read the existing clipboard endpoint, while `TerminalStream` delivers text or cleared frames through the terminal cache for the selected primary task only. Revision fences prevent delayed reads from replacing live events. The copy gesture uses `copyNotice` directly and never clears staging; empty text remains present. Logs exclude payloads and focused clearing returns to the composer.

### Menu icons

Action menus render through shared/row-menu.js. Each entry receives exactly one leading decorative SVG from shared/menu-icon.js, including links and disabled entries. Callers choose a semantic `icon`; entries without a choice retain the shared action icon. Set `danger: true` for a red label/icon and a preceding separator. Menu anchors and buttons share border, padding and icon geometry so labels align. Settings navigation uses the same stroke set, with repository letters retained as identity avatars. Native select options retain browser rendering and keyboard behavior.

TaskSelection owns an explicit ID set independent of the viewer and search. Cascades use the full hierarchy restricted to current search matches, including collapsed tasks. Tri-state controls derive from current scope membership. TaskBatch freezes targets; confirmDialog owns the shared task-list body, tone, progress and Stop for archive, restore and delete, at N=1 or N>1. runTaskBatch caps requests at four and retains separate succeeded, failed and not-started IDs. Stop/auth/network failures stop dispatch while in-flight requests finish. Bulk observations use cached task/roster/PR data only. Single delete alone may enrich with changedFiles and safe PR resolution. Cached header PR links clear on disconnect.
