# Workspace design

## Composition

The standalone client presents a persistent project grouped task rail, a central agent or review area, and a tools rail. Rails collapse independently. Agent focus hides both and restores their previous visibility. The tools rail provides Changes with changed/untracked paths, registered Artifacts and task Info. Unified and split diffs and registered artifact previews use existing API data.

Task selection and review location belong to browser navigation state. Drafts remain in memory by task. Geometry preferences and opt-out remembered credentials may use origin local storage; drafts stay in memory. Settings and task creation are integrated. Unsupported Hera mutations remain excluded.

WorkspaceLayout owns column geometry. Named grid slots keep the main and tools panels in their intended columns when hidden rails leave zero-width tracks; automatic grid placement would instead move them into those collapsed slots. Branch and file labels truncate within their tools panel, and wide viewer content scrolls locally.

## Session and review behavior

The agent view uses the existing raw PTY terminal. Task selection moves the cached terminal view and preserves its session; separately guarded resizing can invoke the daemon's existing Claude rerender path. Browser selection guards prevent late responses from being applied to the wrong selected task, but existing backend input races and error semantics remain unchanged. Visible interactive idle sessions fit after 400 ms of settled geometry, using a fresh REST eligibility check; busy or hidden terminals defer fitting, and explicit Fit remains a fallback.

Review is limited to actual changed and untracked paths, returned unified diff text, split presentation, and registered artifacts. It does not imply full repository browsing or source reads. Existing archive and restore operations retain their current effects; show their consequences and confirm before mutation. Do not add lifecycle guarantees.

## Local serving and security

A standalone development server serves this client on a separate loopback port and proxies `/api/` unchanged to one configured loopback daemon. The proxy is required because the existing CORS response does not expose `X-Output-Total` to a separate browser origin. It does not change the daemon contract. The existing `/vendor/` xterm assets are served read only.

Keep the API token in memory and optionally remember it on this origin after successful login; disconnect and auth rejection clear it. Explicit share-link generation and EventSource URLs are the only credential URL exceptions. Do not log tokens, token-bearing URLs, request bodies, drafts or agent output. Render API text safely. Sandbox registered HTML artifact previews with scripts disabled and do not expose the daemon token to iframe content. No external dependencies, fonts or CDNs are introduced.

## Explicit exclusions

No Go or existing static asset edits, daemon restart, backend/API/schema/contract changes, independent shells, full source browsing, conversation adapters, fabricated context or account allowance data, conditional input, identified git comparisons, or stronger backend error guarantees. The existing web UI remains untouched.

## Session and navigation ownership

| Concept | Owner | Relationship |
| --- | --- | --- |
| Terminal recovery | `TerminalStream` | A reconnect supersedes the prior recovery timer and version; stale work cannot change current recovery state. |
| Changed directory entry | `TaskTools` | Decode the existing git path representation before passing the path to `TaskReview`. |
| Configured project group | `TaskNavigation` | Combine the existing project list with task membership; show empty groups only without a search query. |
| Project list read | `readProjects` | Optional project metadata may fail independently of required tasks; auth and cancellation errors still propagate to connection handling. |
| Task workspace chrome | `WorkspaceTaskView` | Owns the existing task header and terminal status rendering; navigation, token storage and attachments retain their separate owners. |

These boundaries use the current frontend modules and existing authenticated APIs. No new domain layer or backend contract is introduced.

## Attachment inventory

| Concept | Type | Owner and relationship |
| --- | --- | --- |
| Attachment | Value | Local file, preview URL, upload state and confirmed path belong to one task. |
| Attachment store | Entity | Owns task attachment lists, upload cancellation, retry and resource disposal. |
| Attachment policy | Policy | Validates existing upload caps and constructs the explicit message from text and ready paths. |
| Attachment chips | Visual component | Composer shows the selected task's files without rebuilding the native textarea. |
| File drop | Operation | Window guard prevents file navigation; composer and agent panel delegate accepted files to the selected task. |
| Multipart upload | Contract | Existing POST task upload accepts `files` and returns `paths`; no terminal input occurs until Send. |

Use one upload per chip so retries and removal are independent. Keep a draft within the existing batch limits of 20 files, 100 MiB per file and 250 MiB total. Upload timeout is 280 seconds; unconfirmed uploads require checking Files before explicit retry. Removing a chip removes its message reference, not the server file. Image clipboard files take precedence over accompanying text; clipboard content without files follows the existing plaintext path.

## Settings inventory

| Concept | Type | Boundary and relationship |
| --- | --- | --- |
| Settings workspace | Layout | Full-window shell, footer gear, section routing and return to the current task. |
| General settings | Entity/form | Owns KB, defaults and API partial updates; global sandbox is separate. |
| Project settings | Entity/form | Owns project list, full exposed-config replacement and confirmed removal. |
| Project editor | Visual component | Native fields for identity, repository and project sandbox overrides; preserves undisplayed returned fields. |
| Workspace connection | Operation | Owns login, remembered credentials and connection lifetime independently of settings and task navigation. |

Existing task/project rail row ownership is unchanged. Settings section requests are cancelable; writes are explicit and keep the form visible until the response or timeout. Connection lifecycle extraction preserves existing authentication behavior while keeping the app coordinator small.

General partial settings writes are limited to 64 KiB and send only owned fields. KB path and HTTP API enabled changes show the existing restart-required notice. Project bodies are limited to 4 KiB. Re-read project configuration after writes to use normalized overrides. Project removal deletes configuration without deleting tasks or worktrees.

The public `WorkspaceSettings.openProject(name, {history})` entry point restores `settings=repository:<name>` routes after project loading and reports missing projects inline. It reuses the existing editor.

## Grouped Settings navigation

| Name | Kind | Boundary and relationships |
| --- | --- | --- |
| SettingsSection | Contract | Stable id, label, group and create(context) constructor returning a destroyable section view |
| SettingsNavigation | Layout | Searchable grouped section and repository choices; owns project metadata reads, busy controls and selection |
| RepositorySettings | Operation | Uses ProjectSettings to open one named configuration or the add form; retains public openProject(name, options) |
| SettingsSearch | Policy | Case-insensitive filter of navigation labels, repository paths and visible settings rows; never changes saved configuration |

WorkspaceSettings owns view lifecycle/history. SettingsNavigation contains grouped choices and delegates selection to it. Section modules own reads, edits and cleanup through the shared context, keeping scheduler/system modules independent of the shell.

SandboxSettings owns global sandbox form state and its partial settings write. SandboxProposals owns editable proposal cards and explicit approve/reject decisions, preserving path/scope drafts after errors. AgentSettings owns read-only Backend and Account lists, each loaded independently so optional account lookup errors cannot hide configured agents. Both section actors receive the SettingsSection context and abort on destroy.

NotificationSettings contains BrowserPushSettings, DeviceTokens and ShareShortcut. BrowserPushSettings owns permission/subscription and registered-device REST state; its worker only handles push/click events and never intercepts fetches. DeviceTokens owns the master-gated credential roster and transient minted secret. ShareShortcut asks Api to construct a credential-bearing original-UI URL only on explicit generation and keeps it in section memory. All children abort and clear transient output on destroy; the parent serializes mutation controls.

## Maintenance and Update parity

MaintenanceSettings owns explicit bulk cleanup confirmation and the daemon's returned task/worktree/orphan/skipped counts. UpdateSettings owns the saved source path, master-token failures, build output and requested-restart/reload lifecycle. Api remains the credential actor; Update opts into raw error responses only for the existing update endpoint so compiler output can be shown without changing default sanitized errors. Both sections own abort and pending state; neither retries destructive operations automatically.

## Settled automatic terminal fitting

| Actor | Responsibility | Relationships |
| --- | --- | --- |
| TerminalCellSpace | Measured value/policy | Actual client width/height, cell metrics, measured or overlay scrollbar reserve and a small right gutter determine valid proposed PTY cells |
| TerminalGeometry | Observer | Tracks visible pane geometry, reserves the right edge and publishes size/hint state; never sends API writes itself |
| TerminalAutoFit | Controller | Debounces 400 ms, serializes manual and automatic requests, coalesces changes and cancels stale/hidden/cache-suspended work |
| TerminalSessionResize | Operation | Rechecks fresh task/PTY state and latest cell space before the existing API mutation; reconciles already-matching server dimensions without a resize |
| AgentTerminal | Session operation | Verifies fresh task state before automatic resize, retains the existing resize/rerender recovery path and publishes mismatch hints only when auto-fit is skipped |

The viewport's xterm scrollbar owns vertical scrollback. The cached terminal may scroll horizontally for a wider authoritative PTY, reserving scrollbar/gutter space after its last column. Geometry uses the visible host's client box rather than CSS width or an enlarged terminal canvas; recorded/busy terminals keep their authoritative geometry until explicit fitting or an eligible settled auto-fit. Rail and header markup retain separate owners.

## Intentional redundancy consolidation

Changes provides one view with flat/tree controls, keeping changed/untracked directory expansion and both copy-path forms. Info owns task facts; settings retains editable fields plus session ID and sandbox, and the header identifies the task without repeating workflow/elapsed facts. PR actions share safe metadata-first, output-link-second resolution. Repository settings navigation owns the list and Add repository, and project rail Configuration opens directly from a gear. One confirmation dialog, clipboard feedback operation, cached roster reader and explicit creation/move account policy replace duplicate implementations. Settings writes invalidate roster reads; task selection scopes shared output links. The existing Copy worktree path shortcut label remains explicit.

### Project roster refresh

Project roster consumers distinguish reuse within a view from revalidation during a rail snapshot. `readProjects` requests revalidation for initial, periodic and explicit task refreshes. The shared reader replaces settled results for that request while still coalescing an in-flight project read. A previous success must not hide a later server error; the navigation layer retains its last rendered project list and shows the existing warning independently of task success.

## Task actions and connection controls

Task settings owns workflow status and session lifecycle; the captured-task Claude session picker joins Stop/Resume there. Split and Fit retain their existing toolbar ownership. Archive entry points are the rail, settings and shortcut. Task menu keeps task settings, explicit Pin/Unpin and copies; it is the only remaining menu, so no unused shared-menu abstraction is introduced. Pin state consumes an available boolean task field; the existing raw read remains necessary when the DTO omits it, and mutation still re-reads the raw record to preserve fields.

WorkspaceEvents owns the connection Retry surface; the footer is a plain status label and snapshot refresh belongs to the events banner. Session Reconnect retains its terminal-only meaning. Desktop alerts owns the Web Push subscription via the service worker and its permission control. In-page needs-input/idle alert preferences remain distinct from desktop delivery.

| Abstraction | Boundary and relationship |
| --- | --- |
| Pin knowledge | RailActions tracks known booleans separately from pinned membership, reading raw only when supplied task state is insufficient. |
| Captured session actions | TaskSettingsForm groups Stop, Resume and supported session switching for its captured task. ClaudeSessions owns only the picker and cancellation. |
| Connection recovery | WorkspaceEvents owns Retry and snapshot recovery; footer status and session Reconnect retain their scopes. |
| Desktop alerts | BrowserPushSettings owns the one browser permission/subscription control. WorkspaceAttention owns only in-page alerts and badges. |

## Project rail simplification

The rail always uses project groups with coordinator nesting. The Mac Hera sidebar mode and show-Hera-managed filter are intentionally not carried over. Nesting already covers their navigation purpose. Stored rail-grouping preferences are ignored without migration. All tasks remain available through search/archive; a cross-project worker follows its visible coordinator and becomes a root when its parent is filtered out.

TaskNavigation owns search, archive and disclosure state. drawTaskGroups renders projectGroup containers around taskForest roots. taskRoles supplies role tags from live bindings. WorkspaceRoster refreshes only for active role-tag/nesting or split consumers. SplitWorkspace retains its independent toggle, coordinator worker default and compact roster.


## Full-window Settings shell

| Concept | Owner | Relationship |
| --- | --- | --- |
| Settings mode | `WorkspaceSettings` | Preserves the existing route and task attachment, hides and fences workspace chrome until dismissal. |
| Settings navigation | `SettingsNavigation` | Owns Back, searchable grouped entries with icons and the compact grouped selector. |
| Settings page | `WorkspaceSettings` | Gives every registered section a large title and bounded scrolling content frame. |
| Setting row | `settings/settings-fields.js` and `settings/settings.css` | Reuses existing accessible field labels and hints, placing controls on the right and separating rows. |

Settings remains on its existing history route. Closing preserves task/review location and drafts. The whole workspace chrome is hidden and inert during Settings, and previous inert values are restored when leaving. At narrow widths the navigation is a top Back/selector bar, with independently scrolling page content. Complex section content retains its own controls and lifecycle. The slash key focuses desktop Settings search when focus is outside editable fields; Escape retains dialog ownership before closing Settings.

## Compact Changes rows

Changes uses one compact row per file, with a dim directory prefix that yields space to the intact filename, exact text additions/deletions and a status badge on the right. Hover and keyboard focus reveal a small menu chevron. No inline copy actions or discard action appear. Flat/tree is one small icon toggle in the header.

ChangedFileRow owns file identity and diff selection. The existing task-menu popover becomes a shared row-menu primitive for captured actions and keyboard navigation. File actions use copyNotice for relative/absolute paths and an encoded vscode://file link when an absolute worktree exists. ChangedFileCounts observes visible rows and reads existing per-file diffs with at most four requests in flight, cached for this task/status generation and canceled on task changes. Pending/unavailable counts remain blank and binary files show bin. Header totals come from the returned branch/working diff-stat summary lines. No per-file counts are inferred from scaled stat bars. No daemon changes are included.

## Compact task header

The task header composes WorkspaceTaskView identity, TaskHeaderLinks branch/PR links, existing workspace tabs and SplitWorkspace/Fit actions. Keep their current state and request ownership. The header uses existing surface, border, text, spacing and radius tokens with a compact desktop height and a two-line narrow layout. No changes to Changes or rail ownership. Terminal status remains a live region; only warning/error feedback and reconnect occupy visible space.

The existing global layout stylesheet mixes page, rail and task styles. Task header styles live in a dedicated stylesheet.

## Composer alignment and control tooltips

The composer uses the existing composer footer as the single layout owner. Its read-only agent identity and three 32px action targets share one vertical center. Context, attachment and send glyphs occupy 16px boxes; pending send retains that geometry and its accessible Sending message state.

| Concept | Kind | Relationship |
| --- | --- | --- |
| Composer footer | Layout | Contains read-only identity and action controls on one axis |
| Composer action | Visual component | Fixed target containing a centered glyph |
| Control tooltip | Contract | Uses shared delegated data-tooltip and aria-label attributes |
| Context usage summary | Value | Comes from the existing measured or unavailable context snapshot |

The shared tooltip owner is implemented independently. Composer, task header and terminal controls use shared tooltip attributes without native titles; session-control behavior remains unchanged. The model label is not clickable. No REST contract or other frontend changes are needed.

## Shared tooltip and rail footer

TooltipOwner is any dynamically rendered element carrying data-tooltip and an independent aria-label. TooltipBubble is a single text-only description with an optional ShortcutHint. The delegated tooltip controller owns timing, keyboard visibility, viewport placement and aria-describedby lifetime. ConnectionState owns the footer icon's text alternative, color state and tooltip together. RailFooter composes ConnectionState, the existing shortcut overlay trigger and Settings gear in one line. Existing task/menu/disclosure owners retain their actions.

Styles use workspace surface/text/border tokens and reduced-motion preferences. Native title is removed from opted-in controls and task-row ancestors so it cannot compete with the bubble. A labeled search-clear button replaces the browser's unlabelable native search affordance. The existing rail, task hierarchy and Settings owners remain unchanged in responsibility.

## Rail hierarchy geometry

| Abstraction | Responsibility | Relationship |
| --- | --- | --- |
| Rail indent | One 12 px spacing unit | Root task icons align with project avatars. Each child container adds one unit |
| Project children | Nested layout container | Holds the task forest below the project header |
| Task disclosure slot | Reserved row gutter | Parents paint a chevron; leaves reserve the same width |
| Project action icon | 24 px button with a centered 16 px SVG | Gear and plus share stroke, spacing and tooltip contract |
| Role status column | Right-aligned task metadata | Pinned markers precede tags so tags retain one right edge across levels |

The existing project group, task family and compact task row remain the behavior owners. Their shared geometry preserves these boundaries. Selected and attention backgrounds stay on each task row, reaching the right edge from that row's indent. Indentation alone supplies grouping so no additional surface competes with attention or selected backgrounds in either theme.

## Attachment send acceptance

Attachment submission writes one closed paste, waits for output settling within five seconds and sends exactly one standalone CR. Literal image paths are valid input. Successful daemon acceptance of CR clears the captured browser draft and attachments; terminal rendering does not establish recipient acceptance.

| Concept | Kind | Relationship |
| --- | --- | --- |
| Attachment submission | Operation | Owns bounded paste settling and one separate CR |
| Terminal observation | Value | Tracks output advancement and a 100ms quiet window outside transient Pasting status |
| Pending attachment draft | Entity | Remains owned by TaskComposer until the daemon accepts CR |

Attachment intent and uploaded image paths follow the composer, cache and terminal send path. Output that does not settle within the bound permits CR after a final live-session check. Cancellation and input failures before acceptance retain the captured draft and attachments; explicit disconnect keeps its clearing behavior. The client never retries paste or CR automatically. No message text is rewritten or logged. Plain-text send behavior and REST contracts remain unchanged.

## Feature folders

Modules and styles live with their feature, including pure policies. Single-use parts nest under their owner. Shared data and transport live in `web-workspace/core/`. Reused UI primitives live in `web-workspace/shared/`. Unit tests mirror source folders under `web-workspace/tests/`. Browser specs and their shared fixtures live under `web-tests/tests/workspace/` and run through `web-tests/playwright.workspace.config.ts`. The synthetic proxy security suite uses `web-tests/playwright.workspace-security.config.ts`. Entry files and the push service worker retain their root paths. Supporting contracts and context notes live in `web-workspace/docs/`.

## Browser logging disposition

The optional browser client uses the sanitized feature logger in `web-workspace/core/api.js`, emitting `[workspace:area]` events through `console.debug`. Stream, event and live-tool owners log success, failure and skipped work through this browser equivalent of Go uxlog. No daemon logging endpoint is added. Tokens, bodies, drafts and terminal output remain excluded.

## Header identity tooltip

`WorkspaceTaskView.header` retains a native title containing the complete task name and project, including when the name fits. Project metadata makes this an intentional informational exception. Delegated control and rail tooltips continue to use data-tooltip without native titles.
