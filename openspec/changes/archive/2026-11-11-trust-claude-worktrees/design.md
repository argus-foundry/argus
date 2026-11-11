## Context

CreateAndStart owns all fresh production worktrees, including Hera workers, coordinators and sub-coordinators. Account routing finishes before worktree creation. Claude rewrites its global config while sessions run.

## Goals / Non-goals

Prepare trust before launch on every frontend and account. Preserve all unrelated JSON values, including large numbers. Fail open on any error. Do not retrofit existing worktrees, change usage probes or clean up trust at deletion (named follow-up in proposal).

## Decisions

- Add a family registry trust hook, implemented only by Claude; do not dispatch on backend names.
- Run the hook in CreateAndStart after OnWorktreeCreated and before persistence/start. The creation owner (daemon for headless/API/Hera, local TUI when applicable) writes trust; the supervisor does not own creation or this write.
- Resolve the same account and backend CLAUDE_CONFIG_DIR mapping used by launch. Default account uses ~/.claude.json; an explicit config directory uses its .claude.json. Existing account sandbox grants cover that directory; no broader grant is needed.
- Resolve worktree symlinks and normalize NFC to match the installed Claude 2.1.317 trust lookup. Verified from the binary: oYe/XI/ay use hasTrustDialogAccepted. sM/hut prefer the main repository's canonical Git root for linked worktrees, while XI/iy/sy also accept the worktree's own NFC-normalized absolute path; seed that narrower entry. Existing ~/.claude.json confirms absolute project keys.
- Reuse atomicfile's bounded, no-follow object reader and unique temporary-file/rename writer. Decode raw JSON values to preserve unknown values exactly, serialize Argus trust writers within the process, and read afresh for every operation.

## Risks / Trade-offs

- Concurrent Claude writers can still win a read/rename race; atomic rename prevents partial files but is not a cross-application transaction. Keep the critical section short and never cache config contents.
- Malformed JSON or invalid projects/project object shapes are left untouched and logged; creation continues and Claude may show the dialog.
- Trust remains after failed creation or later deletion; account-aware removal is the named follow-up.
