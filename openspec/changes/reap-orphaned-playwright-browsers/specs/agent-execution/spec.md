## ADDED Requirements

### Requirement: Orphaned automation browsers are closed

On macOS, the daemon SHALL check about every 5 seconds for orphaned automation browsers and close them. An orphaned automation browser is a Chrome-family main process that meets all of the following:

- it was launched with `--remote-debugging-pipe`;
- it is owned by Argus;
- its parent is PID 1, meaning the process that launched it has exited.

A browser SHALL count as owned by Argus if either of the following holds:

- the daemon previously observed it, while its launcher was still alive, with an `argus` process among its ancestors;
- its environment contains a non-empty `ARGUS_TASK_ID`.

Closing SHALL send SIGTERM to the browser's process group (or to the process alone if it does not lead its own group). If the same process instance is still alive after a grace period, closing SHALL then send SIGKILL. Before every signal, the daemon SHALL confirm that the process has the same start time it was classified with, so it never signals a reused PID.

A browser whose launcher is still alive SHALL NOT be closed. A Chrome process without `--remote-debugging-pipe`, or one not owned by Argus, SHALL NEVER be signalled.

A steady-state check SHALL make a single process-table read and SHALL NOT spawn subprocesses. Command lines SHALL be read only for Chrome-family main processes that have not already been classified.

Each close and each escalation SHALL be logged. A failed process-table read SHALL be logged, and that check SHALL close nothing.

The check SHALL be controlled by the config.toml key `[browsers] close_orphaned`, defaulting to `true`. When the key is `false`, or config.toml is unreadable, nothing SHALL be closed, but ownership SHALL still be recorded.

#### Scenario: Browser left behind by a killed Playwright server is closed
- **WHEN** an Argus agent's Playwright MCP server launches Chrome and the server is then killed with SIGKILL
- **THEN** within about 5 seconds the daemon sends SIGTERM to that Chrome's process group and its dock icon disappears

#### Scenario: Unresponsive orphan is force-killed
- **WHEN** an orphaned automation browser is sent SIGTERM and the same process is still alive after the grace period
- **THEN** the daemon sends it SIGKILL

#### Scenario: Codex agent's browser is closed
- **WHEN** a Codex agent's Playwright MCP server, whose environment has no `ARGUS_TASK_ID`, launches Chrome while the daemon is running, and the server later dies
- **THEN** the daemon closes that Chrome, because it observed the browser under an `argus` ancestor before it was orphaned

#### Scenario: Browser orphaned during daemon downtime is closed
- **WHEN** the daemon starts and finds an orphaned automation browser whose environment contains `ARGUS_TASK_ID`
- **THEN** the daemon closes it

#### Scenario: User's own Chrome is never touched
- **WHEN** the user's everyday Chrome, launched from the Dock with parent PID 1 and no `--remote-debugging-pipe`, is running
- **THEN** the daemon never signals it

#### Scenario: Browsers from sessions outside Argus are untouched
- **WHEN** an orphaned automation browser has neither an observed `argus` ancestor nor `ARGUS_TASK_ID` in its environment
- **THEN** the daemon never signals it

#### Scenario: Browser in use by a live session is untouched
- **WHEN** an Argus agent's Playwright MCP server is alive and its Chrome is open
- **THEN** the daemon does not close that Chrome

#### Scenario: Reused PID is not signalled
- **WHEN** an orphaned browser exits and its PID is reused by another process before the next signal
- **THEN** the daemon detects the start-time mismatch and sends nothing

#### Scenario: Sweeper can be turned off
- **WHEN** config.toml sets `[browsers] close_orphaned = false`
- **THEN** orphaned automation browsers are left running, as before this change
