# Review and verification

A single-agent `hera-review` and `hera-review-test-adversary` pass checked behavior, delta coverage, call sites, authorization, storage transactions, and regression assertions. No unresolved blocking findings remain.

Fixed findings:

- Require the Bearer scheme for decision sessions and test both non-Bearer rejection and expiry through HTTP.
- Ignore poll responses begun before a submit or refresh, and disable inputs while submitting so a late edit cannot appear recorded without being sent.
- Preserve locally cached cards and revision-keyed drafts after session expiry; keep session credentials exclusively in sessionStorage.
- Strengthen structured-render width checks, test every registered route against decision-session denial, test post-commit event visibility, and test full-note preservation plus bounded summary truncation at maximum batch size.

Pending-delta audit:

| Capability | Implementation and evidence |
| --- | --- |
| coordinator-board | Structured DB/MCP round trips, validation, omitted/empty options, material edits, TUI renderer and hint tests; builtin and orientation guidance. |
| board-decision-page | Standalone embedded page, safe markup/answer/draft/outcome JS tests, transactional batch/outbox/event tests, phone-width and dark-mode browser checks. |
| rest-api | Ticket/session lifetime and reuse tests, all-route denial matrix, device/master/plugin checks, strict JSON validation, mixed outcomes, secret-free log capture. |
| keybindings | Default and rebound decide action, collision/inventory checks, generated help and README. |
| hera-view | Any-row/no-selection dispatch, remote inertness, API-disabled/empty/error launcher tests and SimulationScreen key dispatch. |
| mobile-pwa | Service-worker network bypass, shell exclusion and version bump assertions. |

All touched Go package suites and all 794 workspace JS tests passed before the full gate. Live-daemon end-to-end interaction and process argv inspection remain unverified; see tasks 9.1 and 9.3. Browser tests used response fixtures, and launcher tests used an httptest server and fake opener.

## Local gate recovery

- The single full `make pre-pr` passed build, vet, format, lint and vulnerability checks, then heavy race packages hit the 300-second local guard. The coordinator approved a `GOFLAGS=-p=3 make test-cover-gate` rerun; DB/API/TUI still needed isolated runs with CI’s ten-minute deadline. All 794 workspace tests passed.
- Isolated `TestNukeHeraRole` passed with race in 2.755s. The full isolated DB race suite passed in 320.959s (1341 cases); its five slowest top-level tests were `TestDB_ErrorPaths` (12.39s), `TestHeraMessageSend` (5.73s), `TestDB_InsertMessage` (5.52s), `TestReviveHeraWorkerToInProgress` (4.25s), and `TestArchiveHeraUnit_ReverifiesEveryClauseInTx` (4.11s), all pre-existing.
- Ten repeated race+coverage DB-open checks found no setup regression from the two new additive columns: current median/mean 200/201ms, baseline overlay without the columns 200/202ms.
- Fixed the pre-existing pending-graduation test race by routing header reads/writes through `readUI`. Re-recorded the supervisor spawn digest and bumped spawn surface 54→55 because the builtin Hera guidance is materialized by the supervisor; stream is unchanged. The new guidance reaches new coordinators after a supervisor restart at Aaron’s convenience; board/MCP/page functionality works independently.

- The isolated API race suite passed in 462.242s (2332 cases). Its five slowest tests were `TestWtDescriptorCycles` (37.47s), `TestWorktreeWatcher` (16.12s), `TestCloseoutGuard` (10.32s), `TestWtHubStatus` (9.51s), and `TestRTProbe` (7.50s), all pre-existing. Focused race checks passed for the corrected supervisor digest/current-surface semantics and the strengthened JSON/log contracts.
- A browser regression check confirmed that after a conflict, editing the retained note and reloading into an expired session still shows and copies the latest note; the fragment is absent and credentials are absent from localStorage.

- The isolated TUI race suite passed in 386.119s (2321 cases), including the fixed pending-graduation test (0.32s). Its five slowest tests were `TestSync2026_BeforeAfterMeasurement` (8.49s), `TestSmoke_HeraBoardWheelScrollTallItem` (5.16s), `TestCrossTabArrows` (4.83s), `TestSmoke_ProjectFirst` (3.71s), and `TestPaneDrawPath_NoConfigNoEvaluationAtSteadyWidth` (3.59s), all pre-existing.
- Replaced the timed-out DB/API/TUI portions of the full race profile with the complete passing isolated profiles, as approved by the coordinator. The standard filter passed at **89.2%** (56,697 / 63,593 statements, 13 filtered files), above the 88% floor. The supervisor pin/current-surface checks and final decision JSON/log checks passed separately with race.
