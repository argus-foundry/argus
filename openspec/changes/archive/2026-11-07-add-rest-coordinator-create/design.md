## Decisions

- **Reuse `SpawnHeraCoordinator`.** It already creates the orchestrator, task, role and binding with LIFO compensation. The handler never hand-rolls creation. `Profile` is added to its input and forwarded to `CreateAndStart`; the TUI call site passes nothing, so TUI behavior is unchanged.
- **Receipts in their own table.** `hera_coordinator_requests(request_id, body_hash, state, response, ...)` uses `CREATE TABLE IF NOT EXISTS` like the other hera tables. A generic `operations` table exists on an unmerged sibling branch; once both merge, this table could fold into it. No cross-branch dependency was taken.
- **Receipt flow.** The claim (insert pending) is atomic under the DB mutex. Same id and same canonical body: completed returns the stored response with status 201 and `Idempotent-Replay: true`; pending returns 409. Same id and a different body returns 409. A failed spawn releases the pending receipt so the id can retry. If the process dies mid-spawn the pending receipt stays and the id keeps returning 409 (conservative: the unwind did not run, so the caller must use a new request_id).
- **Canonical body.** Fields are trimmed and re-marshalled without `request_id` before hashing, so key order, whitespace and UUID case do not matter.
- **Unknown JSON fields are rejected (400)**, which is how `command` and `worktree` inputs are refused.
- **Response profile/branch** are the resolved values (project binding / default branch); `task.Profile` stores only an explicit override.
- **Deviation: "unsupported backend".** The ticket lists it as a 400. The existing coordinator rules accept any configured backend (warning for non-Claude), so the 400 applies to backends that are not configured. No explicit-model validation was added: `model` is passed through under the existing rules.
- **Auth.** Existing API authentication (any valid token), matching `POST /api/tasks`.
- **Profiles listed** are those that load and validate from the shared library plus the project checkout's `.argus/profiles`; `archetypes` is the canonical set.
