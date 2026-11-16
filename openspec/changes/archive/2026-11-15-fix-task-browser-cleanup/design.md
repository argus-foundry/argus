## Context

Read-only host inspection on 2026-11-15 found Chrome 68419 (PPID 55005, PGID 68419; parent PGID 54793) in Sketch/rai-roi-guests-2, and Chrome 96924 (PPID 1, PGID 96924) in Sketch/prod-browser-session/scripts. Both task rows were live/in_progress. Headless shell 99122 (PGID 99122) belonged to orphan node 99118 (PPID 1, PGID 99118), cwd Sketch/kit-table-sticky-header, whose task row was missing. SESS was 0 throughout. Temporary profiles were generic playwright_chromiumdev_profile paths with no Argus marker. No signals were sent. The earlier report of seven Chrome instances was not reproduced: this snapshot contained two full Chrome browser roots and one headless shell.

Installed Playwright coreBundle.js:10359-10362 explicitly uses detached:true; isolated MCP launch at 76315 disables handleSIGTERM. These explain why a session-group signal does not guarantee browser cleanup. sandbox-exec replaces itself on exec, so absent sandbox-exec ancestry does not establish absence of an inherited profile. Outgoing AppleEvent grants do not explain a local menu event; normal Quit failure remains unproven without interactive reproduction. Chromium's AppController still enables IDC_EXIT and calls CloseAllBrowsersAndQuit.

Both surviving task rows have sandboxed=1 (read-only SQLite). Chrome 68419 was headed with --no-sandbox and --disable-blink-features=AutomationControlled; Chrome 96924 had --headless. Both used --remote-debugging-pipe and --no-startup-window. Thus the automation-controlled flag alone cannot explain the report, and the inherited Argus sandbox is distinct from Chrome's own disabled sandbox.

## Goals / Non-goals

- Clean up task-owned browser roots and captured Chrome helpers without relying on parentage or session groups.
- Preserve personal Chrome, live tasks, deliberate resumes, and processes whose ownership cannot be verified.
- Non-goals: changing outgoing AppleEvent permissions, asserting an unverified Quit root cause, controlling attached personal-browser CDP sessions, Firefox/WebKit support, or signaling the existing live processes during development.

## Decisions

- Use a small browser-process package with injectable command/signal/wait seams. Require recognized executable, Playwright temporary profile or managed-worktree profile, remote-debugging pipe, and cwd strictly under the configured managed worktrees root. A shared Chrome crashpad directory is never ownership evidence.
- Snapshot browser-group helper identities before signaling. Re-read each PID's argv and cwd immediately before TERM and KILL; signal individual PIDs, never an unverified process group. New browser launches during a grace period are outside the snapshot.
- Resolve existing filesystem aliases in managed-root/worktree comparisons, including macOS /var versus /private/var and symlink escape rejection. Process-inspection subprocesses have a three-second timeout.
- Full session stop takes the snapshot before signaling the agent, then cleans asynchronously. Worktree deletion cleans before removal. Agent-only/rerender stops preserve browsers.
- Run a separate process-driven daemon sweep every 10s, with six eligible ticks and checked task/running reads. All worktree claimants must be finished with no live session; a missing row is eligible only without a live claimant. Recheck before escalation; unknown state fails closed.
- Doctor reports orphaned task browsers read-only. All three frontends share the daemon lifecycle behavior; no UI parity work is required.

## Risks / Trade-offs

- Cwd discovery can fail or point outside a worktree → skip and log, never guess from a shared cache or generic temporary profile.
- Processes can disappear or PIDs be reused → compare captured argv and cwd before each signal; the kernel signal race cannot be eliminated with portable PID APIs.
- Crashpad helpers lack a task marker and have their own groups → deliberately excluded rather than risk personal Chrome.

## Migration plan

Restart the supervisor for the changed stop path and daemon for the sweep. Roll back by reverting the commit and restarting them. Existing task-owned browser roots can be discovered by cwd; no schema migration or persistent launch marker is needed.

## Open questions

Normal Quit failure requires an approved interactive reproduction; no cause is asserted from argv alone.
