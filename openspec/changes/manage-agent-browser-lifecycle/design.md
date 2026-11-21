## Context

On Aaron's Mac, normal browser jobs should run without opening Chrome windows. A deliberately visible automation window should close when its job finishes, or after a short idle period if the agent forgets. Personal Chrome and Claude-in-Chrome must remain untouched.

The read-only November 19 survey and current code agree: `BuildCmd` sets a cache directory and disables the nested Chromium sandbox, but does not select a browser or headless mode. `browserReaper.eligible` protects unfinished/live tasks. `Scan` currently abandons all results after one root cwd error. `Session.StopScoped(StopTree)` and worktree removal capture browsers; natural exit needs an explicit, generation-scoped path.

The locally installed MCP 0.0.86 depends on Playwright 1.66.0-alpha-2026-11-18, whose manifest identifies revision 1255 as Chrome for Testing / Chrome Headless Shell. Its configuration merges file, environment, then CLI options; defaulting environment variables alone cannot override every project/plugin CLI. Its public `createConnection(config, contextGetter)` API can serve an Argus-owned context. These are source observations, not results of browser experiments.

References: [Playwright MCP](https://github.com/microsoft/playwright-mcp) and [browser version coupling](https://playwright.dev/docs/browsers). The installed sources are a candidate pin, not evidence that an alpha build is the best supported release.

## Goals / Non-goals

**Goals:**

- One resolved policy for sandboxed Claude and Codex, including fresh/resumed/recycled launches and both process owners.
- Explicit browser-job ownership, deterministic close on completion/cancellation, and a finite idle fallback that never interrupts active calls.
- Testable precedence, bundle selection, generation fencing, and per-candidate scanner failure behavior.
- Local telemetry sufficient to correlate a later Dock bounce or crash with a task and browser engine, without collecting page data.

**Non-goals:**

- Editing global/project/plugin source configuration outside this repository.
- Proving that branded Chrome caused the reported crashes; retained evidence does not establish that.
- Changing WebKit test coverage, prohibiting every possible executable an agent can launch, or rewriting arbitrary third-party scripts.
- Closing pre-existing unmanaged windows merely because their task is in review. Those lack trustworthy operation activity; existing full-stop/orphan cleanup remains available.
- New frontend controls in this PR. `browser-policy-settings-parity` names the follow-up for browser settings/status UI on TUI, web, and macOS. Current shared stop/restart/remove controls all receive the lifecycle fix.

## Decisions

### 1. Own the launch and pin the entire runtime

Add a small managed Node/TypeScript runtime behind `argus browser mcp` and `argus browser run <script>`. Go resolves policy once per agent generation; both commands use that same policy. Use an exact MCP/Playwright lockfile and its matching browser manifest revision, installed into a versioned Argus cache. Do not depend on the project's node_modules, the host npx cache, or `@latest`. Review vendor approval before installing any dependency. Missing/mismatched bundles produce an actionable error; never fall back to `/Applications/Google Chrome.app`.

Default owned launches use `chromium.launch` with the matching headless shell and explicit `headless: true`, no branded channel, isolated context, and `chromiumSandbox: false` only under Argus's outer sandbox. Explicit headed requests select the matching full bundled build. Assert canonical executable paths under the selected revision. Existing required sandbox grants are retained; this change does not broaden filesystem access or introduce a new network listener.

Before implementing against a chosen pin, verify its supported API, bundle paths on macOS arm64/x64 and Linux, and license/approval status. Record exact version, integrity and revision in the PR. The observed alpha pin is not silently promoted to the shipping choice. A pin that changes the approved design comes back for review.

Alternative: environment-only headless defaults are smaller, but CLI overrides and unpinned plugins defeat them. Changing the existing orphan reaper alone cannot identify browser-job completion.

### 2. Resolve MCP precedence per launched process

Extend the existing `Family` launch integration, rather than scatter backend-name conditionals. Deliver one named managed Playwright server per sandboxed Claude/Codex launch. Resolve effective browser entries from account/user, project, and enabled-plugin scopes. Replace known local Playwright launchers in task-scoped overrides and suppress duplicate launching aliases, including namespaced plugin entries; preserve non-browser tools and explicit CDP/extension attachment integrations. Source configuration remains byte-for-byte unchanged.

Claude delivery uses the existing per-launch MCP/settings path; Codex uses per-launch configuration and its Argus home overlay. Exact precedence and plugin suppression must be verified against installed CLI versions with isolated fixture homes before declaring this complete. A same-name override is not assumed to suppress a namespaced plugin. If a CLI cannot selectively suppress a conflicting browser server, report a concrete conflict and keep browser automation unavailable for that launch rather than silently claiming policy enforcement; do not globally disable unrelated tools. Ask Aaron before any broader configuration-isolation design.

Use a fixture matrix covering user-only, project override, enabled plugin, duplicate aliases, disabled entries, malformed config, account homes, fresh/resume/recycle, and both supervisor/in-process execution. Include a non-launching CLI inventory check after approval. Existing outside-Argus configuration alignment remains a PR follow-up list.

### 3. Track browser work at the operation boundary

The managed MCP adapter owns the browser/context and uses the supported context-getter interface. Track MCP request IDs, in-flight count, cancellation, last settled activity, ownership kind, and generation. Provide an explicit job-finish operation alongside normal browser-close support; routing guidance tells agents to finish browser work in success and failure paths. Do not close after every tool call: navigation followed by inspection is one job.

The direct runner exposes an owned context to a script and wraps execution in `try/finally`, closing on success, exception, cancellation, and process termination. Guidance covers raw Playwright scripts too: bundled Chromium, headless by default, required nested-sandbox option, and `finally` cleanup. Arbitrary scripts bypassing the runner do not gain managed idle guarantees; do not pretend environment variables enforce script options.

Model states as unopened → idle/open → busy → idle/open → closing → closed. A single serialized lifecycle gate prevents a timer close from racing a new operation. A new job after close gets a fresh isolated context. Concurrent calls increment/decrement activity correctly; cancellation does not declare an operation idle until it has unwound. Explicit cancellation can abort and close the cancelled job, but may not affect another job/generation. Set bounded operation deadlines so hung cancelled requests do not retain a browser forever.

### 4. Idle means no browser operation, not no terminal output

Propose `browser.idle_timeout_seconds = 300`, accepting positive bounded values (60–3600 seconds); invalid values reject configuration instead of disabling cleanup. Timer starts after the last operation settles, pauses while any operation is in flight, and rechecks ownership, generation and activity atomically before closing. Headed and headless owned sessions follow the same timeout. Task status, MCP keepalives and agent terminal output do not renew it.

Closing an idle owned context does not stop the agent. Its next browser request lazily opens a fresh context and reports that previous browser state expired. Explicitly attached browsers have no idle-close timer and are never targets of process escalation; releasing a connection must not invoke an operation that closes the remote browser or user tabs.

Headed mode remains available through an explicit per-job request (`headed: true` for managed MCP job start; `argus browser run --headed <script>` for direct work). Routing requires a user request for visible interaction/debugging; no automatic headed retry after a headless failure. Human inspection longer than the timeout requires an explicit finite renewal or a longer configured timeout; no permanent keep-open switch.

### 5. Cleanup belongs to a launch generation

Record owned browser identity under a task/generation with browser PID plus OS process start identity, canonical executable, unique profile and owner relationship. Maintain a supervisor/local-runner lifecycle handle, so daemon restarts do not imply browser work is finished. Completion, cancellation, transport EOF, full task stop, natural agent exit, restart replacement, and worktree removal trigger idempotent closure of that generation's owned resources.

Graceful context/browser closure comes first. Capture identities before terminating owners or removing worktrees; retain them for TERM then five-second KILL fallback. Reverify PID start identity, executable/argv, cwd/profile and generation before each signal. Never discover new children by worktree alone during old-generation escalation. A resumed/new generation invalidates stale cleanup against its resources. Agent-only/rerender restarts preserve active browser operations when their owner survives; if transport genuinely ends, close only the old adapter's resources and lazily reopen on reconnection.

The existing orphan sweep remains the conservative safety net for unmanaged leftovers. Candidate cwd/read failures skip only that candidate and invalidate only its own grace evidence. Whole `ps`, task-list, or running-list failure remains fail-closed for the entire tick. Doctor distinguishes partial inspection from a clean empty result. Add exact Chrome for Testing/headless shell/helper fixtures rather than matching arbitrary executables containing “Chrome.” A cache path alone never establishes ownership.

### 6. Attribute failures without logging secrets

Emit structured local launch/start/settle/close/exit and scan-skip events through uxlog and the existing logging path. Allowlist task ID, generation/job ID, owner PID, browser PID/start identity, engine, package/build revision, resolved bundle-relative executable, mode, ownership kind, durations, in-flight count, exit code/signal, and enumerated reason/error class. Never log raw argv, environment, URLs, headers, page content, cookie/storage data, script arguments, or unfiltered upstream errors. A launcher child failure must still have the prelaunch task/generation event. Record detected WebKit as WebKit rather than infer Chrome from a Dock icon.

## Implementation evidence (2026-11-21)

- Installed Claude 2.1.330 supports `deniedMcpServers` in process-scoped settings, including exact `plugin:<plugin-name>:<server-name>` aliases. Prefer suppressing classified local-launch aliases and adding one reserved managed server name; do not use `--strict-mcp-config`, which also removes unrelated servers. See the [settings reference](https://code.claude.com/docs/en/settings-reference#deniedmcpservers).
- Installed Codex 0.169.0 supports process-scoped `mcp_servers.<alias>.enabled=false` and `plugins."<plugin-id>".mcp_servers.<server>.enabled=false`. Plugin transport cannot be replaced through plugin policy, so disable the conflicting alias and register the managed server separately. Exact-release evidence: [plugin policy type](https://github.com/openai/codex/blob/rust-v0.169.0/codex-rs/config/src/types.rs#L1086) and [loader enforcement](https://github.com/openai/codex/blob/rust-v0.169.0/codex-rs/core-plugins/src/loader.rs#L1378). Do not write the Codex overlay's symlinked configuration. Fixture verification is still required.
- The observed MCP context-getter API does not close the supplied default context when `browser_close` runs. The adapter must explicitly close owned resources and reset the lazy context; isolated job semantics come from the supplied context, not the MCP `browser.isolated` flag (which requests unsupported `SimpleBrowser.newContext`). Cancellation must settle through a handler-level finally path, because cancelled SDK requests can suppress outgoing responses.
- Dependency approval is unresolved: on 2026-11-21, both a Playwright-filtered query and the full Keystone approved-server registry contained no Playwright MCP entry. Under the supplied security policy, no new MCP package installation or runtime materialization will run pending verifiable approval through #ai-help-desk. Existing cached source was inspected read-only. Independent scanner work can proceed without this dependency.

## Risks / Trade-offs

- CLI/plugin precedence can change → fixture and installed-version inventory checks gate release; unknown conflicting launchers are reported instead of silently bypassing policy.
- MCP adapter API/version coupling → exact lock, public API only, fake-transport tests, and isolated smoke verification after approval.
- Idle expiry loses transient browser state → explicit finish, visible expiry message, configurable finite timeout, and lazy reopen.
- Old uninstrumented windows can remain until their owning session stops → report that rollout limit; do not infer their idleness from task status.
- Replacing the supervisor interrupts agents → separate production deployment confirmation and report affected sessions first. No automatic live supervisor bounce from this worktree.
- Headless removes ordinary windows but cannot promise zero Dock activity from unrelated apps, WebKit, or explicitly headed requests → collect attributable telemetry and state the guarantee narrowly.

## Migration plan

After plan approval: tests first, implementation, review plus test-adversary pass, focused tests and full `make pre-pr`, OpenSpec validation, archive on the branch, then PR/CI. Before any production change, obtain the required deployment confirmation.

Deploy the pinned adapter/assets with the Go binary. Verify installation path, runtime manifest, both running daemon and supervisor build hashes/protocol surfaces, and a fresh sandboxed Claude/Codex task's effective server inventory. A binary mtime or daemon SHA alone does not prove the supervisor changed. Run disposable browser jobs only after approval, use test-owned profiles, and compare process inventory before/after. Verify default executable, sequential cleanup, idle headed cleanup, long-running operation preservation, attached-browser protection, stop/natural-exit/restart/remove, and daemon-bounce survival.

Rollback selects the previous coherent Go/runtime pair and safely stops only new managed owners; it does not alter external configuration or personal browsers. The PR lists follow-ups for ai-ron, Sketch, MerchantUI, sherlock and official plugin configuration: remove `@latest`, align exact package/bundle, select headless bundled Chromium, retain nested-sandbox settings where applicable, and use explicit finish/idle policy.

## Review decision

Aaron approved the proposal through Plannotator folder-review session `66c5b81b-8169-4375-afd4-14a3f62dc656` on 2026-11-19. No separate timeout answer was submitted; the proposed five-minute default stands. The reviewed choice and tradeoffs are retained below as the decision record.

### Question 1 of 1: Idle window lifetime

How long should an unused automation window remain open after its last browser action finishes?

This applies only to browsers Argus owns. A running browser operation pauses the timer; personal Chrome and attached browsers are excluded. Visible mode remains an explicit per-job choice and follows the same timer.

- **Five minutes (approved plan default):** clears forgotten windows promptly, but long manual inspections may need renewal.
- **Fifteen minutes:** gives more inspection time, but forgotten windows remain longer.
- **One minute:** minimizes leftover windows, but pauses between browser steps can lose state.

Recommended: Five minutes

The design proposes explicit headed mode because visual debugging remains useful; making it a request per job avoids restoring accidental visible launches. Removing headed support entirely would prevent that debugging workflow. No separate decision is needed unless Aaron wants to change this proposed behavior during review.
