## 1. Process scan and classification (internal/agent)

- [ ] 1.1 Write tests for pure classification against fabricated snapshots:
  - the Chrome-family comm filter (main-process names match, `Google Chrome He…` helpers never match);
  - argv parsing of a `kern.procargs2` buffer into argv and env;
  - the `--remote-debugging-pipe` gate;
  - the `ARGUS_TASK_ID` env marker;
  - the ancestry walk to an `argus` comm, with cycle and missing-parent safety.
- [ ] 1.2 Implement the darwin snapshot (`unix.SysctlKinfoProcSlice("kern.proc.all")` reduced to pid, ppid, pgid, comm and start time) and the `kern.procargs2` reader. Add a non-darwin build that returns no processes.
- [ ] 1.3 Implement a classifier with a `(pid, starttime)` cache pruned to the current snapshot. It reads argv only for unclassified comm-matched candidates and records ownership from observed ancestry or the env marker.
- [ ] 1.4 Implement the identity-checked signal (re-read kinfo; require the same start time and ppid 1; signal `-pid` when it leads its group, otherwise the pid) with test seams for the snapshot and kill functions.

## 2. Daemon sweeper (internal/daemon)

- [ ] 2.1 Write tests for the tick:
  - an owned orphan gets SIGTERM, then SIGKILL after the grace period only if the same identity is still alive;
  - a live launcher is skipped;
  - an unowned browser is skipped;
  - a browser without the pipe flag is skipped;
  - a scan error closes nothing;
  - config off or unreadable closes nothing but ownership is still recorded;
  - a Codex-style browser (no env marker) is closed after observed ancestry;
  - shutdown cancels a pending escalation.
- [ ] 2.2 Implement `browserreap.go`: a 5s ticker, a SIGTERM→SIGKILL escalation goroutine cancelled on shutdown, and `[browserreap]` slog plus uxlog logging. Wire it into daemon startup next to the finished-session and dev-stack sweepers.
- [ ] 2.3 Add `BrowsersConfig{CloseOrphaned bool}` (default true; absent key means on) to `internal/config`, with a loader test, and read it live each tick.

## 3. Verification and docs

- [ ] 3.1 Add a benchmark or test that pins the steady-state tick to one process-table read with no argv reads for already-classified PIDs.
- [ ] 3.2 Do a manual end-to-end check on the host: launch Chrome via `@playwright/mcp`, SIGKILL the server, and confirm the browser closes within about 5s while the user's own Chrome stays open. This is offline from the TUI, with no live A/B capture.
- [ ] 3.3 Add a gotcha bullet to `context/knowledge/gotchas/daemon-rpc.md` and a row to the index table, and add the new config key to the README Reference config table.
- [ ] 3.4 Run `make pre-pr` clean, then archive the change (merge the delta into `openspec/specs/agent-execution/spec.md` and move the folder to `changes/archive/`).
