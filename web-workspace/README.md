# Theia

A separate desktop web client using the existing Argus API. The original web UI, daemon, TUI and native app are unchanged. No daemon restart is required.

Start with Node 20 or newer from the repository root.

```sh
node web-workspace/dev-server.mjs
```

Open `http://127.0.0.1:7750` and enter an existing Argus API token in the browser. Remember on this device is checked by default. After successful connection the token is saved in this origin's localStorage and refresh reconnects automatically, matching the original UI's security posture on this loopback-only client. Uncheck it for a memory-only token. Task drafts always stay in tab memory and are lost on refresh. Disconnect clears credentials and drafts. Never paste a token into chat. The gear beside connection status opens Settings.

The frontend binds only to loopback. Its development proxy defaults to `http://127.0.0.1:7743` and forwards the existing API without adding operations. It preserves terminal cursor headers that are not exposed through direct cross-origin browser requests. To use another local daemon, start with `--api http://127.0.0.1:PORT`; use `--port PORT` for the frontend port.

## What works

Project grouped tasks, search and archive access, browser Back and Forward for task and review selections, independently collapsible rails, agent focus mode, keyboard and pointer panel sizing, real agent terminal output and input, per-task drafts, unified and split diffs, changed or untracked paths, registered artifact previews and task Info. Archive and restore use existing task actions after confirmation.

Visible interactive idle terminals fit automatically after window or pane changes settle for about 400 ms. Fresh REST task state gates the resize because the daemon's Claude rerender path may stop/resume a process. Hidden/cache-suspended and known busy terminals defer fitting; Fit session remains a manual fallback. Wider authoritative output can scroll horizontally until fitting is safe.

If the project list cannot load, tasks remain usable and a warning offers refresh. Refresh retains the last loaded project groups until the list can be loaded again.

Settings includes General, repository configuration, Sandbox, Agents and Notifications & devices. General manages existing KB, default agent/share project and HTTP API preferences. Projects can be added, edited and removed without leaving the workspace. Escape or Back returns to the task and its draft. Removing a project removes configuration only; existing tasks and worktrees remain. The current project API resets any diligence profile assignment on edit, and the editor shows that limitation before saving. Schedules and System/Logs use the inventory's independent section modules.

Enter sends from the composer; Shift+Enter inserts a new line. Selecting a task or completing a send focuses its native text box for dictation. Pasted newlines never send on their own. Search, login and direct terminal input retain their own paste behavior.

Drop files or screenshots onto the composer or agent panel, or paste an image into the composer. Chips show upload state, image previews, retry and removal. Uploads stay with the selected task even if we switch tasks. Send includes the uploaded paths with the message; uploads never submit on their own. Finish, retry or remove pending/error chips before sending. Attachment sends allow terminal paste processing to settle for up to five seconds, then send one separate Enter. The draft clears when the daemon accepts that input. Terminal output is not a delivery acknowledgment, so Enter is never retried automatically. The limits are 20 files per draft, 100 MiB per file and 250 MiB total.

Dropping files uploads them immediately into that task's `.context/` directory. Removing a chip removes its message reference, while the uploaded file stays in the task. If an upload times out, check Changes before retrying because it may have completed. Chips and previews stay in tab memory and clear on disconnect or refresh.

## Limits

There are no independent shell terminals, cleaned conversation views, full repository tree, arbitrary source reads or invented context metrics. Existing backend input races, replay limits and git error ambiguity remain. The output view uses the available bounded history. Reconnect refreshes that snapshot so a reset process cursor does not silently skip replacement output; reading position is restored on a best effort basis. Multiline compose requires the agent terminal to enable bracketed paste. After an unconfirmed send, check the terminal before resending. Registered Markdown displays as source. Sensitive drafts are not saved across refresh. A workspace-local service worker handles Web Push notifications; there is no offline shell cache or public hosting workflow.

Archive clears intertask message records and can lead to an idle agent being stopped by the existing reaper. It does not mean completion or immediate worktree deletion. Restoring does not promise to recover cleared messages or restart an agent.

## Verification

Run unit tests from the repository root with Node 20 or newer. CI and `make pre-pr` include these tests; `make workspace-test` runs them on their own. No dependency install is needed.

```sh
npm test --prefix web-workspace
```

Browser tests use the existing `web-tests` Playwright dependency and runner. Build the existing isolated harness, then let Playwright own the harness and workspace proxy.

```sh
go build -o /tmp/argus-test-server ./cmd/argus-test-server
npm ci --prefix web-tests
(cd web-tests && npx playwright install chromium webkit)
(cd web-tests && npx playwright test -c playwright.workspace.config.ts)
(cd web-tests && npx playwright test -c playwright.workspace-security.config.ts)
```

The workspace config owns ports 17744 and 17750, with harness reset on 17754. The security config runs afterward and owns a synthetic upstream and proxy on 17744 and 17750. Neither reuses an existing server or accesses the live daemon, real tokens or real Argus data. Live-write suites retain the temporary harness worktree checks. Browser fixtures use synthetic clipboard events without reading the OS clipboard. Native macOS screenshot thumbnail dragging remains a manual dogfood check.

See [test commands](docs/testing.md) for focused runs and the master suite. No browser suite is wired into CI or the Makefile.

## Remembered token

The workspace uses `argus-workspace-token`, separate from the original UI's `argus-token` on its own origin. A rejected saved token (401 or 403) is removed and login asks for a valid token. Network and server errors retain it and offer Retry connection. Failed login never stores the attempted token. Blocked browser storage falls back to memory-only operation. Pagehide disposes viewers without deleting a remembered token. Only the token can persist; drafts remain memory-only.

## Nested task navigation

Bound Hera workers appear under their coordinator with recursive disclosure controls. Click the chevron, or focus it and press Enter or Space, to hide or show descendants. Collapse choices last through task refreshes and filter changes in this tab, and reset on reload or disconnect, like project groups. A parent signals descendants needing input even while collapsed. Tasks without a visible parent remain ordinary rows; search and Archive keep their matching tasks reachable. Descendants in another project follow their parent's group. If the roster cannot load, a warning accompanies flat task navigation.

See `web-tests/tests/workspace/workspace/rail/nest.spec.ts` with the same isolated frontend/browser settings for nesting, keyboard focus, search, archive, history and roster-failure regressions. The rail archive checks run in `workspace.spec.ts`.

Task rows use one line with a labeled status icon and a truncated name. Active sessions spin unless reduced motion is enabled. PRs are green, completed or terminal PRs purple, failed Hera roles red, and input attention retains its warning color. The API combines merged and closed PRs, so their icon says PR merged or closed; ordinary task failure is not available in the task payload. See `web-tests/tests/workspace/workspace/rail/task-row.spec.ts` for icon, compact layout, ellipsis and reduced-motion checks.

The final red Delete entry in each task menu opens a task review with advisory session, Hera, changed-file and PR details. Cancel is initially focused. Deletion is permanent and requests forced worktree and local/remote branch cleanup in the background.

Select beside the task count enters bulk mode. Task bodies toggle individual selection without changing the viewer or draft. Native tri-state select-all, project and coordinator checkboxes select matching tasks through collapsed trees. Active offers Archive; Archived offers Delete, including live or Hera-bound tasks. Space toggles a focused checkbox, Up/Down moves between visible checkboxes, and Escape or Cancel exits. Search retains selection and announces hidden selected tasks; changing Active/Archived clears it. All single and bulk actions share one confirmation with capped attention and ordinary-task groups. Up to four requests run concurrently with progress and Stop. Failures remain checked with their reasons for inspection before an explicit retry.

## Project actions

Project rows show a letter avatar, a plus button and a Configuration gear on hover or focus. Empty projects stay visible without an expand control or task count. The plus button opens a main-pane form for that project with a prompt and optional name. Start task uses daemon defaults and opens the created agent. Enter starts the task; Shift+Enter adds a line. Pasted text never submits. Errors retain the prompt, while Cancel or Escape returns to the previous view. If creation was canceled or unconfirmed, refresh tasks before retrying because it may have completed. Creation drafts stay in tab memory and clear on disconnect. Attach files after creating the task. Repository configuration is available in workspace Settings.

See `web-tests/tests/workspace/workspace/rail/project.spec.ts` against the isolated harness for empty-project state, project controls, failed and canceled creation, and actual default-backend task creation and terminal input.

## Composer

The composer uses a roomy rounded draft area with a read-only backend/model label and right-aligned context, attach and send controls. Use + to choose multiple files through the existing upload path. Drag/drop, image paste, attachment chips and native dictation keep the same behavior, without a persistent instruction banner. The selected task's explicit model comes from its raw REST record; an absent model is not replaced with an assumed default.

See `web-tests/tests/workspace/composer/composer-layout.spec.ts` for the bottom-bar geometry, read-only model and native file-picker checks.

Click the context ring for measured context and usage. Live bridge readings can fill the ring; recorded Hera stamps are dated. A dashed ring means context percentage is unavailable. Claude account limits show remaining usage and reset times when exposed. Plan, email and organization remain here when exposed. Account and historical role input, cache and output totals appear in task Info. Unsupported fields stay hidden. The [data availability table](docs/context-data.md) identifies the daemon work needed for Codex quota and full context breakdown parity.

See `web-tests/tests/workspace/composer/composer-context.spec.ts` for context/quota mapping, dismissal, narrow viewport, stale task reads and disconnect checks. Reads refresh every 15 seconds without touching draft input or sending terminal commands.

## Archive context

Archive and restore keep the same task open in the main pane and preserve the rail filter and scroll. A task outside the current filter has no highlighted row; restoring it reselects the row when that filter includes it. The archived terminal stays visible as recorded output with input disabled. See `web-tests/tests/workspace/workspace/archive-context.spec.ts` for shortcut/row archive, scroll retention and restore-in-Archive checks.

## Rail settings

The plus button is the only project creation action. Click a project's Configuration gear to open its existing Settings editor. Each task row also offers an actions menu beside Archive. Task settings can rename the task, change workflow status, archive/restore, fork, confirm stop/resume or deletion, and move a Claude task to another compatible account. Changing workflow status does not start an agent. Copy available task ID, branch or worktree values from the task menu. Backend, model, account, branch, base branch, worktree and created time live in Info. Session ID and sandbox facts remain in task settings; existing endpoints do not provide safe targeted edits for every field.

Deletion also requests worktree and local/remote branch cleanup; its confirmation describes that effect. Account moves may restart the agent and require explicit opt-in to interrupt a busy session. Errors retain edits and ask us to refresh before retrying an unconfirmed operation. Back, Escape and browser navigation return to the previous task or settings view.

See `web-tests/tests/workspace/tasks/settings/task-settings.spec.ts` for captured task actions, confirmations, copy/menu focus, failed updates and stale results. `task-settings-live.spec.ts` verifies rename, workflow, stop and resume against an owned temporary task in the isolated harness. Destructive delete/account cases use browser fixtures.

## Terminal viewport

The terminal viewport follows the center pane from narrow drawers to ultrawide displays. Fit session highlights when its cell dimensions differ from the available space; its tooltip shows the proposed and current geometry. Click it to resize the PTY explicitly. Window, rail and focus changes schedule a settled fit when the terminal is visible and not known busy, with fresh task and PTY-size checks before a resize. A wider existing PTY scrolls inside the terminal.

See `web-tests/tests/workspace/workspace/responsive.spec.ts` for 1280×800, 1920×1080, 2560×1440 and 3440×1440 column/host bounds, rail toggles and fit hints. `web-tests/tests/workspace/workspace/responsive-live.spec.ts` verifies manual fallback on known-busy metadata, content-box fitting and independently confirmed PTY geometry, using the isolated harness. Existing `overflow.spec.ts` and `overflow-live.spec.ts` retain the narrow and long-path checks.

Settings navigation groups Personal, Agents & environment, Automation, System and Repositories. Search finds sections and repositories by name or path. Repository entries open their configuration directly, with Add repository at the end. The grouped sections include General, Notifications & devices, Agents, Sandbox, Schedules, System and Logs.

Schedules under Automation lists recurring and one-shot jobs, with project, backend/model, prompt and enabled state. New schedules are enabled by default. Run now fires the saved schedule into a fresh task/worktree; unsaved form edits are not included. Delete asks for confirmation and leaves already created tasks unchanged. Failed saves retain edits, and mutations are never retried automatically.

System shows available CPU, load, memory, swap, disk, Argus RSS, session and uptime measurements. It polls every two seconds while visible and pauses in a hidden browser tab. Host sleep warnings include the daemon's suggested command and the existing temporary caffeinate command as text only. Logs offers bounded UX/daemon tails with manual refresh and local scrolling.

See `web-tests/tests/workspace/settings/settings-operations.spec.ts` for isolated section and grouped-navigation fixtures covering CRUD, stale reads, plain-text log safety and visibility-scoped polling. These fixtures never access the live daemon or real API token.
Sandbox manages global paths and pending write-path proposals with master-token recovery. Approval changes apply to new sessions; reload global settings before saving paths after a grant. Agents displays existing backend and account metadata read-only, with independent error states.

Notifications & devices provides browser push setup, registered-device removal, confirmed test notifications, master-gated token management and an explicit iOS share URL helper. Generated credentials are cleared when hidden or when Settings closes. Supply the original Argus origin reachable by the sharing device for a share URL; the optional workspace itself does not implement the original share flow. External push transport and native iOS Shortcut behavior require manual dogfooding; tests use mock transport and fixture tokens only.

See `web-tests/tests/workspace/settings/settings-environment.spec.ts web-tests/tests/workspace/settings/settings-devices.spec.ts` for master-token failure, proposal drafts/order, independent agent metadata, push permission/retry/races and transient credential controls. `web-tests/tests/workspace/settings/settings-devices-live.spec.ts` verifies real isolated token mint/revoke/authentication, test-token share generation and local worker activation. It refuses to write outside the temporary harness and never sends a push notification. Use the same Playwright environment and isolated ports documented above.

## Desktop shortcuts

Use Keyboard shortcuts in the task rail for the reference. Browser equivalents use Alt+1/2/3/4 for Agent/Review/Changes/Info and Alt+Shift with N/R/B/A/P/Backspace/F/J/S for task creation, rename, fork, archive, pin, delete confirmation, filter, attention and schedules. Text fields keep native editing. With terminal focus only Cmd+arrows, Shift scroll chords and Cmd+Shift+C/U and Cmd+Shift+Enter are claimed. Global browser equivalents are deliberately inactive in the terminal.

System also includes Maintenance cleanup and Update/source path. Cleanup confirms deletion of completed task data, sessions/logs, worktrees/branches and orphan worktree directories, then reports daemon counts. Update requires a master token and saved source path, confirms the rebuild and daemon restart, and reloads after five seconds when the daemon requests a restart. Closing the section cancels its reload timer. Automated tests stub cleanup and update; they never invoke a real restart or bulk deletion.

See `web-tests/tests/workspace/settings/settings-maintenance.spec.ts` for cleanup/update confirmation, count reporting, master-token recovery, unsaved-path guards, safe build output, incomplete responses and reload/abort lifecycle checks.

## Project rail and split view

The rail groups tasks by project and nests Hera workers under their coordinator, including workers from other projects. Compact rows retain role-status tags for bound tasks. All tasks remain available through search and archive, with pin and task actions. The Mac Hera sidebar mode and show-Hera-managed filter are intentionally omitted because the nested project rail covers navigation. Stored rail-grouping preferences are ignored.

Split view is a separate button on the agent screen and starts off. It works for any task, alongside the project rail. The left stays on the current task; choose any other task on the right. Coordinator tasks can default to their selected worker and offer a compact collapsible roster. Closing split retains both terminals. Use Alt+Shift+D outside text fields, or Cmd+Shift+Enter with terminal focus. The enabled state stays on this device. Selecting a coordinator does not enable split.

## Desktop attention

Theia uses the selected Spectrum eye for its 25px header mark, SVG favicon, 32px and 192px PNG fallbacks, 180px Apple touch icon and notification icon. These assets belong to the optional workspace; the original Argus SPA keeps its icons. Attention counts are composited over the Theia eye. The tab title and favicon show tasks needing input. The rail is the sole task list. Needs-input names and collapsed ancestors gain attention emphasis that clears with the signal. Needs-input page alerts start enabled; idle alerts start disabled. Settings > Notifications & devices owns in-page needs-input and idle preferences. The first three distinct tasks in five seconds alert immediately; later arrivals update one summary. Snapshot/reconnect replay updates badges silently. Desktop alerts is the single desktop delivery control and uses Web Push through this browser’s service worker. The page never creates a second desktop notification.

Live daemon events update task state immediately. A persistent connection banner offers retry without disconnecting or losing drafts. Like the native app, a 30-second reconciliation snapshot covers existing REST mutations that emit no event. The shared roster refresh runs while role tags, nesting or split presentation need it.

The eight most recently used task terminals retain their scrollback when switching tasks, including the selected and split-visible terminals. Older terminals are disposed and load fresh bounded output history when revisited. Switching still cancels pending input, including a composed message's trailing submit. Missing tasks, disconnect and page close release the retained terminals. New task creation offers backend-compatible accounts when multiple choices are available and uses the project's default backend/account unless explicitly selected.

See `web-tests/tests/workspace/terminal/terminal-autofit.spec.ts` for single vertical scroll ownership, last-column visibility at 1280/1728/2544/3440, settled automatic fitting, busy preflight, cache visibility and request coalescing. `terminal-autofit.test.mjs` exercises the controller deterministically without wall-clock waits.

## Consolidated controls

Changes has Flat and Tree presentations of the same changed/untracked paths. Both copy relative and absolute paths; untracked directories still expand through the existing files endpoint. Alt+3 opens Changes. Info owns task facts and output links. The header and Open PR shortcut prefer a safe cached PR URL, falling back to a safe PR output link.

Repositories in Settings navigation is the only project list. Add repository opens creation; a Configuration gear opens one editor directly. Resume starts or reattaches the task session through an endpoint that also guards a pending daemon rerender. All confirmations use the same cancelable modal and all explicit copies report clipboard success or failure.

See `web-tests/tests/workspace/workspace/redundancy.spec.ts` for shared confirmation retry, cancellation, focus and clipboard feedback. The project, Settings and task information suites cover focused editors, action menus, task facts, Changes and Resume.

Task settings groups Stop, Resume and Switch Claude session for the captured task. Split and Fit stay on the agent screen. Workflow status is edited only in task settings. Task menus offer Pin or Unpin, task settings and copies. Archive/restore stays on the rail, in task settings and on its shortcut.

The events banner is the connection Retry surface. The footer shows connection status. Terminal Reconnect still recovers that task's output session. Periodic task snapshots continue revalidating the project roster.

See `web-tests/tests/workspace/tasks/consolidated-controls.spec.ts` for these control boundaries and pin-read behavior. `mac-session.spec.ts` verifies switching a captured task while another task stays selected, and attention/push suites verify that only Web Push owns desktop delivery.


Settings opens as a full-window mode. Back or Escape restores the previous task and review location, including its draft. The left column holds grouped Settings navigation and search (`/` focuses search outside text fields); narrow windows use a grouped section selector beside Back. All existing settings retain their controls and save behavior. `settings.spec.ts` covers full-window routes, context restoration and row containment at 390, 1280 and 2544 px.

## Compact Changes rows

Changes shows one line per file with a dim directory prefix, intact filename, line counts and status badge. Only the directory truncates; very long filenames scroll inside the row. The small header icon switches flat/tree views. Header totals use the API's branch and working diff summaries. Per-file counts load only for visible rows, with at most four concurrent reads, and are reused while switching views. Pending/unavailable counts stay blank; binary files show bin.

Hover or focus a row to reveal its chevron. Its menu offers Copy path, Copy absolute path and Open in VS Code. Absolute actions require a known worktree. Click the file itself to open its diff. Untracked directories still expand lazily. Discard, Finder and Xcode actions are omitted because this browser client has no supported contract for them.

The list uses the API's returned branch and working paths. The daemon chooses its comparison base from upstream/default refs, rather than the task base branch, so a workspace stacked on an integration branch may include its unmerged history. This frontend does not silently filter those paths.

### Compact task header

Task identity, branch copy, PR link, Agent/Review tabs and Split/Fit icon buttons share one compact bar. Hover the task name for its full name and project. At narrow widths the bar wraps to two lines. Terminal status remains accessible; reconnect feedback appears when needed. Existing split shortcuts and automatic fitting remain available alongside manual Fit. When the selected task has staged text, Copy staged text appears beside the terminal with a preview tooltip. Copy preserves the staged value for reuse; Cmd+Shift+C continues copying visible terminal output.

See `web-tests/tests/workspace/tasks/task-header.spec.ts` for desktop height, long-name/branch containment at 390 pixels, keyboard access, branch/PR links, split, manual Fit and reconnect recovery.

## Folder convention

Feature folders own their UI, styles and pure policies. Nest single-use parts under their owner, at most two folders below the workspace root. `core/` holds shared data and transport modules. `shared/` holds reused UI primitives. Unit tests mirror source folders under `tests/`. Browser specs and their shared fixtures live under `../web-tests/tests/workspace/`. `workspace-push.js` stays at the root to preserve its service-worker URL and scope. Contracts and context notes live in `docs/`; the dictation diagnostic lives in `diagnostics/`.

See [test commands](docs/testing.md) for all unit, browser, security and master verification commands.
