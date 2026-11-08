## Context

Playwright MCP (configured globally as `npx @playwright/mcp@latest --isolated --no-sandbox`) launches Chrome through `playwright-core`'s `launchProcess`. That call:

- spawns the browser with `detached: process.platform !== "win32"`, which on macOS makes Chrome the leader of a new process group;
- drives it over `--remote-debugging-pipe` (fds 3 and 4);
- closes it from MCP's exit watchdog (stdin close, SIGINT or SIGTERM leads to `gracefullyCloseAll`, with a 15s fallback) and from a `process.on("exit")` handler that runs `kill(-browserPid, SIGKILL)`.

None of those handlers run if the Node process is SIGKILLed. `Session.StopScoped(StopTree)` SIGTERMs the agent's process group and SIGKILLs it 5s later. Claude Code's own MCP teardown may also kill its stdio servers outright. In either case the browser is reparented to launchd (`ppid == 1`), its pipe is closed, and no process will ever drive or close it again.

Ground truth (2026-11-07, live host): 37 `playwright-mcp` processes were running.

- **17 under Argus Claude agents:** these carry `ARGUS_TASK_ID`.
- **11 under Argus Codex agents:** these do not carry it, because Codex passes a fixed env allowlist to MCP servers.
- **9 under Claude sessions in iTerm:** not Argus.

The user's own Chrome also has `ppid == 1`, since everything launched from the Dock does.

## Goals / Non-Goals

**Goals:**

- Close Argus-launched automation browsers within about 5s of their launcher dying, whichever path killed the launcher.
- Never touch the user's own Chrome, or browsers belonging to non-Argus sessions.
- Negligible CPU: one syscall per tick in steady state.

**Non-Goals:**

- Closing a browser whose agent session is still alive. That browser is still usable, and stopping idle sessions is the finished-session reaper's job.
- Deleting Playwright's leftover temp profile directory (`$TMPDIR/playwright_chromiumdev_profile-*`). Deleting files is a separate risk decision, left as a follow-up.
- Linux or other platforms (a no-op build). Argus's sandbox and agent tooling are macOS-first.
- Changing the user's MCP config (for example forcing `--headless`).
- Making Argus's `StopTree` itself reap out-of-group descendants. The sweeper subsumes it, and a stop-time reap would still miss sessions that end on their own.

## Decisions

**D1. Process-table sweep, not stop-time reap.** A browser can be orphaned by any launcher death, including a crash, a Claude Code teardown, or an agent that exits by itself. Most of those never pass through `StopScoped`. A periodic sweep catches all of them.

**D2. A browser counts as an automation browser only if its main process was launched with `--remote-debugging-pipe`.**

- Cheap pre-filter: the kinfo `p_comm` is one of the Chrome family's main-process names: `Google Chrome`, `Google Chrome fo` (the 16-byte truncation of "Google Chrome for Testing"), `Chromium`, or a `chrome-headless-s…` prefix. Helper processes (`Google Chrome He…`) never match.
- The real gate: argv, read via `kern.procargs2`, contains `--remote-debugging-pipe`. A user-launched Chrome never carries that flag.
- Requiring a Playwright-specific `--user-data-dir` would miss non-isolated (persistent `mcp-chrome-*`) profiles and buy nothing, because ownership (D3) is the safety gate.

**D3. Ownership comes from observed ancestry, or from an `ARGUS_TASK_ID` in the browser's environment.**

- Observed ancestry: each tick, while a candidate's parent is still alive, walk its ppid chain through the snapshot already in hand. If any ancestor's `p_comm` is `argus` (the supervisor, or the in-process runner's TUI), record `(pid, start time)` as owned. This is the only signal that covers Codex agents.
- Env fallback: `kern.procargs2` returns argv and env in one buffer. When a candidate is first classified, a non-empty `ARGUS_TASK_ID=` marks it owned. This covers a browser that was orphaned while the daemon was down, or before its first tick, for Claude agents. Playwright passes `process.env` to the browser unchanged, and Claude Code passes it to MCP servers.
- The accepted gap: a Codex-launched browser orphaned during daemon downtime has neither signal and is left alone, failing safe.

**D4. Orphaned means the parent is PID 1.** If the Playwright server is alive, the browser's parent is that Node process and Playwright is responsible for it. `ppid == 1` together with ownership and the D2 flag means no live process can close it. The sweeper closes it on the first tick it sees that state, with no extra dwell; Playwright's own graceful path finishes before the launcher exits, so there is nothing to wait for.

**D5. Identity-checked group signal.**

- Before each signal, re-read the PID's kinfo and require the same start time (`p_starttime`) and `ppid == 1`, so a reused PID is never touched.
- Signal `-pid` when the browser leads its own group (`pgid == pid`, which Playwright's `detached` guarantees), so renderer, GPU and crashpad helpers go with it. Otherwise signal only the PID.
- SIGTERM first (Chrome shuts down cleanly and its dock icon disappears). Then, after `orphanBrowserKillGrace` (5s), SIGKILL if the same identity is still alive.
- The escalation runs on its own goroutine, cancelled on daemon shutdown, so a slow browser never stalls the tick.

**D6. Cost bound.**

- Steady-state tick: one `unix.SysctlKinfoProcSlice("kern.proc.all")`, which measured at about 1.4k processes and well under a millisecond, plus a filter on `p_comm`.
- `kern.procargs2` is called only for comm-matched main processes not already in the classification cache. The cache is keyed `(pid, starttime)` and pruned to PIDs present in the current snapshot.
- No `exec` of `ps` or `pgrep`, and no gopsutil per-PID walk (unlike `rerenderProcessSnapshot`, which reads every PID).

**D7. Config.**

- `[browsers] close_orphaned` (bool) lives in config.toml only and is read live each tick through the existing config loader, with an absent key meaning `true`, mirroring `[devstack] stop_finished`.
- An unreadable config.toml means the sweeper closes nothing that tick and logs the error once (deduplicated), as the dev-stack sweeper does.
- Ownership recording continues while the sweeper is off, so turning it on later still knows about Codex-launched browsers.

**D8. Placement.** The sweeper runs in the daemon, next to `finishedreap` and `devstackreap`, on its own 5s ticker. The daemon has no `uxlog.Init`, so it logs through `slog` (`[browserreap]` prefix) and also through `uxlog.Log` per the repo logging rule. It logs each close, each SIGKILL escalation, scan failures, and the config-off transition.

## Risks / Trade-offs

- **[Risk] Killing a browser someone still wants.** An orphaned pipe-driven browser can no longer be automated, but the user might be looking at the page it shows. → Mitigation: this is exactly the clutter the user asked to remove. The setting turns it off.
- **[Risk] p_comm names drift across Chrome versions or channels.** A missed name fails safe (nothing is closed). → Mitigation: a table-driven name list with tests, and the argv flag remains the true gate.
- **[Risk] `argus` comm match on an unrelated binary named `argus`.** An attacker-free, single-user host makes this negligible. It would also need `--remote-debugging-pipe` and an orphaned state.
- **[Trade-off] Codex browsers orphaned during daemon downtime are missed** (D3). This is accepted as fail-safe.
- **[Trade-off] Temp profile directories still leak.** This is a named follow-up (Non-Goals).
