# REST API

## Purpose

The REST API exposes the Argus daemon's tasks, terminal sessions, configuration, git status, and uploads over HTTP so mobile devices and external scripts can drive the daemon remotely. It is designed for single-user remote control over Tailscale: it refuses to listen on untrusted LANs, authenticates every API request with a bearer token, and reserves destructive or configuration-mutating operations for the master token while allowing per-device tokens to perform per-task operations.
## Requirements
### Requirement: Network binding refuses untrusted networks

The server SHALL bind to the loopback address (`127.0.0.1`) and, when a Tailscale CGNAT address is detected, additionally bind to that address. It SHALL NEVER bind to `0.0.0.0`. Loopback binding is mandatory; if it cannot bind within the retry window the server SHALL return an error. Tailscale binding is best-effort: failure to detect or bind the Tailscale address SHALL leave the server running on loopback only rather than aborting startup.

#### Scenario: Loopback bind succeeds on the requested port

- **WHEN** the requested port is free
- **THEN** the server binds `127.0.0.1` on that port and reports the port it bound

#### Scenario: Requested port in use advances to the next port

- **WHEN** the requested port is already in use but a subsequent port within the retry window is free
- **THEN** the server binds the next free port and reports that actual port

#### Scenario: All candidate ports occupied

- **WHEN** every port in the retry window is occupied
- **THEN** the server returns an error explaining the bind failure that unwraps to the underlying syscall error

#### Scenario: Tailscale address resolves only legitimate CGNAT addresses

- **WHEN** Tailscale address discovery returns a non-CGNAT address such as `0.0.0.0`
- **THEN** the discovery result is rejected and treated as "no Tailscale address" so the server binds loopback only

### Requirement: Authenticated API access via bearer token or query param

Every `/api/*` route SHALL require authentication. The server SHALL accept either an `Authorization: Bearer <token>` header or a `?token=<token>` query parameter, with the header taking precedence. A request with no recognized token SHALL be rejected with 401. A small set of routes that the browser must fetch before login (the dashboard, the share target, vendored static assets, the PWA manifest, the service worker, and icons) SHALL be served without authentication.

#### Scenario: Valid master token accepted

- **WHEN** a request presents `Authorization: Bearer <master-token>`
- **THEN** the request is authenticated and tagged with `X-Argus-Auth: master`

#### Scenario: Missing or wrong token rejected

- **WHEN** a request to an `/api/*` route presents no token or an unrecognized token
- **THEN** the server responds 401 Unauthorized

#### Scenario: Query-param token accepted for EventSource

- **WHEN** a request presents a valid token only via the `?token=` query parameter and no Bearer header
- **THEN** the request is authenticated

#### Scenario: Bearer header wins over query param

- **WHEN** a request presents a valid Bearer header and a wrong `?token=` query parameter
- **THEN** the request is authenticated using the Bearer header

#### Scenario: Unauthenticated dashboard load

- **WHEN** a request hits the dashboard route or a vendored static asset without a token
- **THEN** the asset is served without requiring authentication

### Requirement: Per-device and plugin-scoped tokens

The server SHALL accept non-revoked per-device tokens persisted as SHA-256 hashes in addition to the master token. A device token (empty scope) SHALL be tagged `X-Argus-Auth: device`; a token bound to a non-empty scope SHALL be tagged `X-Argus-Auth: scope:<name>`. Revoked tokens SHALL be rejected. The plaintext token SHALL be returned only at mint time.

#### Scenario: Device token authenticates and is tagged device

- **WHEN** a request presents a valid, non-revoked device token
- **THEN** the request is authenticated and tagged `X-Argus-Auth: device`

#### Scenario: Plugin-scoped token tagged with its scope

- **WHEN** a request presents a valid token minted with scope `ludwig`
- **THEN** the request is authenticated and tagged `X-Argus-Auth: scope:ludwig`

#### Scenario: Revoked token rejected

- **WHEN** a request presents a token that has been revoked
- **THEN** the server responds 401 Unauthorized

### Requirement: Master-only gating for destructive and configuration endpoints

Endpoints that mutate shared configuration or act across all tasks SHALL require the master token: project CRUD, backend CRUD, the config snapshot read (`GET /api/config`), settings writes (`PUT /api/settings`), token minting/listing/revocation, stop-all sessions, prune-completed, and the cleanup-candidates clean endpoint. A request authenticated with a device token or a plugin-scoped token SHALL be rejected with 403 for these endpoints. The curated settings read (`GET /api/settings`) SHALL remain available to any authenticated token, including device tokens. Per-task operations (stop, delete, archive, rename, set-status, write input) SHALL remain available to any authenticated token. The cleanup-candidates compute-trigger and list (read) endpoints are NOT master-gated — they are read/trigger operations available to any authenticated token, matching the per-task-operation tier rather than the bulk-mutation tier.

#### Scenario: Device token rejected from a master-only endpoint

- **WHEN** a request authenticated as `device` calls a master-only endpoint such as stop-all, token minting, project/backend CRUD, or the cleanup-candidates clean endpoint
- **THEN** the server responds 403 Forbidden

#### Scenario: Master token permitted

- **WHEN** a request authenticated as `master` calls a master-only endpoint
- **THEN** the endpoint executes and returns its normal success status

#### Scenario: Plugin scope token does not satisfy master

- **WHEN** a request authenticated as `scope:<plugin>` calls a master-only endpoint
- **THEN** the server responds 403 Forbidden

#### Scenario: Device token allowed for per-task operations

- **WHEN** a request authenticated as `device` calls a per-task operation such as stop or delete
- **THEN** the operation executes (subject to task existence)

#### Scenario: Settings read available to a device token

- **WHEN** a request authenticated as `device` calls `GET /api/settings`
- **THEN** the server returns the curated settings view (the read is not master-gated)

#### Scenario: Device token allowed to trigger or read cleanup-candidate classification

- **WHEN** a request authenticated as `device` calls the cleanup-candidates compute-trigger or list endpoint
- **THEN** the operation executes normally — only the clean endpoint is master-gated

### Requirement: Task creation

The server SHALL create a task from a JSON body containing `prompt`, `project`, and optional `name`, `backend`, and `sandbox_override` fields. `project` SHALL be required. At least one of `name` or `prompt` SHALL be required. When `name` is empty it SHALL be synthesized from the prompt (first 40 characters, newlines/tabs replaced with spaces) and an asynchronous rename may follow. A `backend` that is not configured SHALL be rejected before any worktree is created. A `sandbox_override` that is not one of `""`, `"enabled"`, or `"disabled"` SHALL be rejected before any worktree is created. On success the server SHALL respond 201 with the new task id, name, and status. Requests with a `multipart/form-data` body SHALL additionally accept file attachments.

#### Scenario: Creates a task

- **WHEN** a valid create request with name, prompt, and project is submitted
- **THEN** the server responds 201 with the task id and name

#### Scenario: Missing project rejected

- **WHEN** a create request omits `project`
- **THEN** the server responds 400 Bad Request

#### Scenario: Missing name and prompt rejected

- **WHEN** a create request supplies neither `name` nor `prompt`
- **THEN** the server responds 400 Bad Request

#### Scenario: Unknown backend rejected

- **WHEN** a create request names a backend that is not configured
- **THEN** the server responds 400 Bad Request

#### Scenario: Backend override persists to the task

- **WHEN** a create request names a configured backend
- **THEN** the created task records that backend

#### Scenario: Sandbox override persists to the task

- **WHEN** a create request sets `sandbox_override` to `"enabled"` or `"disabled"`
- **THEN** the created task records that override and it is reflected in the task's resolved sandbox state

#### Scenario: Invalid sandbox override rejected

- **WHEN** a create request sets `sandbox_override` to a value other than `""`, `"enabled"`, or `"disabled"`
- **THEN** the server responds 400 Bad Request

### Requirement: Task listing and retrieval with runtime idle state

The server SHALL list tasks and SHALL support optional filtering by `status`, `project`, and `archived` (`0`/default excludes archived, `1` returns only archived, `all` returns both). For each task the server SHALL derive a runtime `idle` flag that is true only when the task is `in_progress` and either has no live session or its session is waiting for input; non-`in_progress` tasks SHALL never be reported idle. Retrieving a task that does not exist SHALL return 404.

#### Scenario: Lists all non-archived tasks

- **WHEN** a list request omits the archived filter
- **THEN** the server returns the non-archived tasks

#### Scenario: In-progress task without a live session reports idle

- **WHEN** a task is `in_progress` but has no live session
- **THEN** its listed entry reports `idle: true`

#### Scenario: Non-in-progress task never idle

- **WHEN** a task is `pending`
- **THEN** its listed entry reports idle false

#### Scenario: Filter by status and project

- **WHEN** a list request supplies `status` and/or `project` filters
- **THEN** only tasks matching the filters are returned

#### Scenario: Get a missing task

- **WHEN** a get request names a task id that does not exist
- **THEN** the server responds 404 Not Found

### Requirement: Task lifecycle transitions

Stopping a task SHALL stop its session (if any) and set its status to `in_review`. Resuming a task SHALL start or reattach a session and set status to `in_progress`, except that an actively-working (non-idle, live-session) `in_progress` task SHALL be refused with 409; idle or ghost `in_progress` tasks SHALL be resumed (healed) rather than refused. Setting status SHALL accept only `pending`, `in_progress`, `in_review`, `complete` and reject any other value. Renaming SHALL require a non-empty name. Deleting SHALL remove the task, its session, logs, and worktree/branch. Forking SHALL create a new task inheriting the source's prompt/project/backend when not overridden. All of these SHALL return 404 when the target task does not exist.

#### Scenario: Stop flips status to in_review

- **WHEN** a stop request targets an existing `in_progress` task
- **THEN** the session is stopped and the task status becomes `in_review`

#### Scenario: Resume refuses an actively-running task

- **WHEN** a resume request targets an `in_progress` task with a live, non-idle session
- **THEN** the server responds 409 Conflict

#### Scenario: Resume heals a desynced live session

- **WHEN** a resume request targets a task whose DB row drifted off `in_progress` while a live session still exists
- **THEN** the server reattaches, re-syncs the row to `in_progress`, and reports the resume as healed

#### Scenario: Set status rejects unknown value

- **WHEN** a set-status request supplies a status outside the allowed set
- **THEN** the server responds 400 Bad Request

#### Scenario: Rename rejects empty name

- **WHEN** a rename request supplies a blank name
- **THEN** the server responds 400 Bad Request

#### Scenario: Delete removes the worktree and branch

- **WHEN** a delete request targets a task with a worktree and branch
- **THEN** the task is removed and its worktree directory and branch are cleaned up

#### Scenario: Fork requires a project

- **WHEN** a fork request omits a project and the source task has none
- **THEN** the server responds 400 Bad Request

#### Scenario: Lifecycle action on a missing task

- **WHEN** a stop, resume, rename, set-status, archive, or delete request names a task id that does not exist
- **THEN** the server responds 404 Not Found

### Requirement: Terminal output, input, and resize

The server SHALL return a task's terminal output, preferring the on-disk session log and falling back to the live ring buffer, advertising a resume cursor (`X-Output-Total`) and source (`X-Source`) so a client can resume a stream without gap or overlap. A task with neither a log nor a live session SHALL return 404 for output. Writing input and reading/setting PTY size SHALL require a live session and return 404 otherwise. Resize SHALL reject zero dimensions and dimensions out of range (greater than 1000).

Writing input SHALL accept an optional origin indicator distinguishing human-typed input from system-injected input (see the `agent-execution` capability). The indicator SHALL default to human origin when absent — the endpoint's only behavior before this indicator existed — so every pre-existing caller (including plugin-scoped tokens that never learn about the indicator) is unaffected. Only a recognized system-origin value SHALL classify the write as system-injected; any other value SHALL also default to human origin.

#### Scenario: Output served from the on-disk log with resume cursor

- **WHEN** an output request targets a task that has a session log on disk
- **THEN** the server returns the log tail, sets `X-Source: log`, and sets `X-Output-Total` to the full log size

#### Scenario: Tail bound does not change the resume cursor

- **WHEN** an output request asks for a tail smaller than the full log
- **THEN** the body is the requested tail but `X-Output-Total` still advertises the full file size

#### Scenario: No log and no session

- **WHEN** an output request targets a task with no log file and no live session
- **THEN** the server responds 404 Not Found

#### Scenario: Input or size without a live session

- **WHEN** a write-input, get-size, or resize request targets a task with no live session
- **THEN** the server responds 404 Not Found

#### Scenario: Resize rejects invalid dimensions

- **WHEN** a resize request supplies zero or out-of-range dimensions on a live session
- **THEN** the server responds 400 Bad Request

#### Scenario: Absent origin indicator defaults to human origin

- **WHEN** a write-input request carries no origin indicator
- **THEN** the write is classified as human origin, advancing both the session's work-cycle and user-input timestamps

#### Scenario: System-origin indicator classifies the write as system-injected

- **WHEN** a write-input request carries the recognized system-origin indicator
- **THEN** the write is classified as system origin, advancing only the session's work-cycle timestamp

### Requirement: Configuration CRUD for projects and backends

The server SHALL expose master-only CRUD for projects and backends. Creating a project SHALL require non-empty name and path; creating a backend SHALL require non-empty name and command; updating a backend SHALL require a non-empty command. The full configuration snapshot SHALL be retrievable (master-only) and SHALL include defaults, backends, and projects.

#### Scenario: Create a backend

- **WHEN** a master request creates a backend with name and command
- **THEN** the backend is persisted and the server responds 201

#### Scenario: Create backend rejects empty name

- **WHEN** a master request creates a backend with an empty name
- **THEN** the server responds 400 Bad Request

#### Scenario: Config snapshot includes core sections

- **WHEN** a master request reads the configuration snapshot
- **THEN** the response contains defaults, backends, and projects

### Requirement: Git status and diff are path-traversal safe

The server SHALL return git status, file diff, and file-tree listings scoped to a task's worktree. These SHALL return 404 when the task or its worktree is unknown. Diff and file-tree path parameters SHALL reject absolute paths and any path containing `..` with 400, so the diff cannot be coerced into reading files outside the worktree.

#### Scenario: Diff rejects absolute or dotdot paths

- **WHEN** a git-diff request supplies an absolute path or a path containing `..`
- **THEN** the server responds 400 Bad Request

#### Scenario: Git status for an unknown worktree

- **WHEN** a git-status request targets a task with no worktree
- **THEN** the server responds 404 Not Found

### Requirement: File uploads enforce size and count caps

The server SHALL accept multipart file uploads (per task and at task creation) and SHALL enforce a per-file cap of 100 MB, a per-request total cap of 250 MB, and a maximum of 20 files. Uploaded filenames SHALL be sanitized (directory components, control characters, bidi overrides, and leading dashes removed) and written into the worktree without clobbering existing files (auto-suffixed). Oversize or too-many uploads SHALL return 413; empty or invalid-name parts SHALL return 400.

#### Scenario: Oversize attachment rejected

- **WHEN** an upload contains a file exceeding the per-file or per-request size cap, or exceeds the file count cap
- **THEN** the server responds 413 Request Entity Too Large

#### Scenario: Empty or invalid attachment name rejected

- **WHEN** an upload contains an empty file part or a part whose name sanitizes to nothing
- **THEN** the server responds 400 Bad Request

#### Scenario: Filename collision auto-suffixed

- **WHEN** an uploaded file's sanitized name already exists in the destination directory
- **THEN** the saved file is given a numeric suffix so the existing file is not overwritten

### Requirement: Settings partial update is master-only

The server SHALL return a curated settings view (sandbox, KB, API, defaults) and SHALL apply partial updates where each absent section means "leave unchanged". Settings updates SHALL require the master token. Apple-event bundle IDs SHALL be filtered through validation before persistence so invalid entries are dropped. Log retrieval SHALL be limited to the whitelisted log names and SHALL return an empty body rather than 404 when a log file does not yet exist.

#### Scenario: Settings update requires master

- **WHEN** a settings update is requested without the master token
- **THEN** the server responds 403 Forbidden

#### Scenario: Missing log file returns empty body

- **WHEN** a log read targets a whitelisted log that does not yet exist on disk
- **THEN** the server responds 200 with an empty body

#### Scenario: Unknown log name rejected

- **WHEN** a log read targets a name that is not whitelisted
- **THEN** the server responds 400 Bad Request

### Requirement: Hera orchestration roster endpoint

The REST API SHALL expose `GET /api/hera`, a read-only endpoint returning the Hera orchestration roster: a list of orchestrators — each with `id`, `name`, `pinned`, `archived`, `kanban_status` (`active`/`backlog`/`blocked`/`done`), `subtree_cost_usd`, `bridge_parent_orch_id` and `bridge_parent_role_id` (both null when the orchestrator is top-level, both set to the parent orchestrator/role when it is nested beneath another orchestrator via a worker→coordinator bridge), `subtree_needs_input` (a boolean rollup, true when any role in the orchestrator's subtree needs input), and its non-freelance `roles` — plus a top-level `freelance` list of hoisted freelance roles. Each role SHALL carry `role_id`, `orch_id`, `name`, `kind` (`coordinator`/`worker`/`freelance`), `status` (`idle`/`working`/`blocked`/`done`, or empty when no status row exists), `task_id`, `task_name`, `task_status`, `live`, `ready_to_close`, `archived`, `needs_input` (a boolean mirroring the same daemon-authoritative idle-detection signal that drives `GET /api/tasks` and the SSE events stream), `tokens_input`, `tokens_cache_write_1h`, `tokens_cache_write_5m`, `tokens_cache_read`, `tokens_output`, and `cost_usd` (omitted or null when the role's resolved model has no rate-table entry for its accrued usage, or when its token totals are all zero — see `cost-estimation`). Both `cost_usd` and `subtree_cost_usd` SHALL be the PERSISTED, already-priced `cost_usd_accrued` values described in `cost-estimation`'s accrual-time-stamping requirement — the endpoint SHALL NOT compute or reprice cost against a live rate table on read. The endpoint MUST be authenticated like every other `/api/*` route. The handler MUST source all data from the database; nesting/bridging computation (canonical-parent resolution, `bridge_parent_orch_id`/`bridge_parent_role_id`, `subtree_needs_input`) MUST use the shared, tview-free `internal/hera/model` package rather than importing `internal/tui/hera` directly, keeping tview/tcell out of the API binary.

**Scope note (unchanged from prior scope):** `subtree_cost_usd` at THIS endpoint SHALL still be the sum of the orchestrator's OWN roles' cost only (every kind, including nuked ones) — it SHALL NOT recurse into orchestrators nested beneath it, even though `bridge_parent_orch_id` now exposes the same underlying bridge relationship. Extending `subtree_cost_usd` itself to recurse remains a separate, still-deferred follow-up; this change does not alter that field's semantics, only adds the new nesting/needs-input fields alongside it.

`kanban_status` continues to be emitted as-is for every orchestrator regardless of nesting (a nested orchestrator's own, rail-inert `kanban_status` value is still visible in its envelope) — but a client can now distinguish top-level from nested orchestrators itself via `bridge_parent_orch_id`, rather than needing to infer it. `subtree_cost_usd`, `bridge_parent_orch_id`/`bridge_parent_role_id`, `subtree_needs_input`, `needs_input`, and every per-role cost/token field are read-only: mutating any of them directly is out of scope — `GET /api/hera` stays read-only in every field; mutations go through the dedicated endpoints in "Hera mutation endpoints".

These fields SHALL be populated regardless of which client renders them: the native TUI itself reads through this endpoint in `--remote` mode, and the web SPA renders no nesting/needs-input Hera UI yet (an explicit, separately-tracked follow-up, not a reason to omit the data here).

Derived from: `internal/api/hera.go` (`heraOrchJSON`, `heraRoleJSON`, `handleHera`), `internal/hera/model` (`BuildModel`, extracted from the former `internal/tui/hera/model.go`).

#### Scenario: Empty roster

- **WHEN** an authenticated client requests `/api/hera` with no orchestrators present
- **THEN** the response is `{"orchestrators": [], "freelance": []}`

#### Scenario: Bound role surfaces task fields

- **WHEN** a role has a live binding
- **THEN** that role appears under the orchestrator's `roles` with `live: true`, its hera `status`, and the bound task's `task_id`/`task_name`/`task_status`

#### Scenario: ready_to_close surfaces from task_meta

- **WHEN** a bound role's task carries `meta:hera.ready_to_close=true`
- **THEN** its `ready_to_close` field is `true`

#### Scenario: kanban_status defaults to active

- **WHEN** an orchestrator has never had its kanban status explicitly set
- **THEN** its envelope's `kanban_status` field reads `"active"`

#### Scenario: kanban_status reflects an explicit value

- **WHEN** an orchestrator's kanban status has been set to `"blocked"`
- **THEN** its envelope's `kanban_status` field reads `"blocked"`

#### Scenario: Missing or invalid auth is rejected

- **WHEN** `/api/hera` is requested without a valid bearer token or `?token=`
- **THEN** the request is rejected before any data is read

#### Scenario: A role's cost fields reflect its accumulated token totals

- **WHEN** a role's live or ended binding carries nonzero raw token totals and a nonzero persisted `cost_usd_accrued`
- **THEN** the role's JSON carries those five token totals and its persisted `cost_usd`, with no rate-table lookup performed by this endpoint

#### Scenario: An unmeasured role carries no cost figure

- **WHEN** a role's token totals are all zero, or its resolved model has no rate-table entry
- **THEN** its `cost_usd` field is omitted or null, not `0`

#### Scenario: An orchestrator's subtree_cost_usd sums its own roles, including a nuked one

- **WHEN** an orchestrator has two roles with recorded cost, one of which has since been nuked
- **THEN** its `subtree_cost_usd` field includes both — the live role's and the nuked role's

#### Scenario: subtree_cost_usd does not recurse into a nested sub-coordinator

- **WHEN** an orchestrator bridges to a nested sub-coordinator via a worker row, and that nested orchestrator has its own recorded cost
- **THEN** THIS orchestrator's `subtree_cost_usd` reflects only its own roles' cost — the nested orchestrator's cost is NOT added in (see the scope note above; the TUI's local-mode `Model.SubtreeCostUSD` computes the full recursive figure separately, not through this endpoint)

#### Scenario: A top-level orchestrator carries no bridge parent

- **WHEN** an orchestrator has no worker role bridging into it from another orchestrator
- **THEN** its `bridge_parent_orch_id` and `bridge_parent_role_id` fields are both null

#### Scenario: A nested orchestrator surfaces its bridge parent

- **WHEN** an orchestrator is nested beneath another orchestrator's worker role via a coordinator bridge
- **THEN** its `bridge_parent_orch_id` and `bridge_parent_role_id` identify that parent orchestrator and role

#### Scenario: subtree_needs_input rolls up from any role in the subtree

- **WHEN** any role within an orchestrator's subtree (including nested sub-orchestrators reached via bridges) currently needs input
- **THEN** that orchestrator's `subtree_needs_input` field is `true`

#### Scenario: A role's needs_input mirrors the daemon's idle-detection signal

- **WHEN** a role's bound task is flagged by the daemon's idle-detection system as needing input
- **THEN** that role's `needs_input` field is `true`, and `false` otherwise

### Requirement: Reliable pane-delivery endpoint

The system SHALL expose a `POST /api/tasks/{id}/notify` endpoint that registers a text delivery for the named task via the reliable notify service. The request body SHALL require `text` (non-empty string), `submit` (must be `true`), and `delivery_id` (non-empty identifier, max 128 bytes, alphanumeric and `-_` only). An optional `deadline_ms` field controls the delivery deadline in milliseconds (default 300,000; minimum 1,000; maximum 3,600,000). The endpoint SHALL be callable by any authenticated token (master, device, or plugin-scoped). On success it SHALL return the delivery_id and its current state (`"submitted"` or `"pending"`). Re-posting a previously submitted delivery_id SHALL be idempotent (200 with state `"submitted"`).

#### Scenario: Delivery registered and pending

- **WHEN** a client posts a valid notify request for a task whose session is not yet idle
- **THEN** the endpoint returns 202 with `{"delivery_id": "...", "state": "pending"}`

#### Scenario: Delivery submitted inline (session already idle and unfocused)

- **WHEN** a client posts a valid notify request for a task that is idle and unfocused at request time
- **THEN** the endpoint returns 202 with `{"delivery_id": "...", "state": "submitted"}`

#### Scenario: Re-post of submitted delivery_id is idempotent

- **WHEN** a client posts a notify request with a delivery_id that was already submitted
- **THEN** the endpoint returns 200 with `{"delivery_id": "...", "state": "submitted"}` without re-injecting

#### Scenario: Missing or invalid fields rejected

- **WHEN** a client posts a notify request with missing text, missing delivery_id, or `submit` not `true`
- **THEN** the endpoint returns 400 with an error identifying the missing or invalid field

#### Scenario: Delivery_id format rejected

- **WHEN** a client posts a notify request with a delivery_id containing characters outside alphanumeric and `-_`
- **THEN** the endpoint returns 400

#### Scenario: Task not found

- **WHEN** a client posts a notify request for a task ID that does not exist
- **THEN** the endpoint returns 404

#### Scenario: Any authenticated token accepted

- **WHEN** a request authenticated with a device token or a plugin-scoped token posts to this endpoint
- **THEN** the request is accepted (not rejected as master-only)

### Requirement: Cancel pane-delivery endpoint

The system SHALL expose a `DELETE /api/tasks/{id}/notify/{delivery_id}` endpoint that cancels a pending delivery registered via the notify endpoint. If the delivery is pending, it SHALL be removed and the response SHALL indicate `cancelled: true`. If the delivery is not found (already submitted or never registered), the response SHALL indicate `cancelled: false` and return 200 (idempotent).

#### Scenario: Pending delivery cancelled

- **WHEN** a client calls DELETE for a delivery_id that is currently pending
- **THEN** the response is `{"delivery_id": "...", "cancelled": true}` and no further PTY write occurs for that delivery

#### Scenario: Already-submitted delivery cancel is a no-op

- **WHEN** a client calls DELETE for a delivery_id that was already submitted
- **THEN** the response is `{"delivery_id": "...", "cancelled": false}` with 200 (no error)

#### Scenario: Unknown delivery_id is a no-op

- **WHEN** a client calls DELETE for a delivery_id that was never registered
- **THEN** the response is `{"delivery_id": "...", "cancelled": false}` with 200 (no error)

#### Scenario: Any authenticated token accepted

- **WHEN** a request authenticated with a device token or plugin-scoped token calls DELETE on this endpoint
- **THEN** the request is accepted (not rejected as master-only)

### Requirement: System metrics endpoint

The API SHALL expose a read-only `GET /api/system-metrics` endpoint, authenticated
like other API routes, that returns a snapshot of host-system load: overall CPU
utilization percent, the 1/5/15-minute load average, total/used/available memory
with percent, total/used swap with percent, total/used/free disk for the filesystem
holding the Argus data directory (`~/.argus`) along with that path, the Argus
process resident memory, host uptime, and the count of active and idle agent
sessions. Each metric SHALL carry an availability indicator so a metric the host
platform cannot supply is reported as unavailable rather than failing the whole
response. Metrics SHALL be sampled by a background collector on its own interval and
served from cache so requests return promptly and CPU deltas are accurate; the
session counts SHALL be read live at request time.

#### Scenario: Authenticated request returns a metrics snapshot
- **WHEN** an authenticated `GET /api/system-metrics` request is made
- **THEN** the response is 200 OK with a JSON body containing CPU, memory, swap, disk, process, uptime, and session-count fields

#### Scenario: Unauthenticated request is rejected
- **WHEN** a `GET /api/system-metrics` request is made without a valid token
- **THEN** the response is 401 Unauthorized

#### Scenario: Unavailable metric degrades gracefully
- **WHEN** the host platform cannot supply a particular metric (e.g. load average)
- **THEN** that field is marked unavailable in the response and the remaining metrics are still returned with 200 OK

#### Scenario: Session counts are read live
- **WHEN** the snapshot is served
- **THEN** the active/idle session counts reflect the runner's current state at request time, not the cached sample time

### Requirement: Cleanup-candidate classification endpoints

The system SHALL expose endpoints to trigger and read an on-demand, daemon-side classification of tasks matching the stuck-task predicate (`archived=1`, `status=in_review`, no live Hera binding): `POST /api/maintenance/cleanup-candidates/compute` starts a background classification pass (a no-op if one is already in flight) covering eligible tasks without a cached verdict or with a non-terminal (not-safe) cached verdict, and `GET /api/maintenance/cleanup-candidates` returns the current cached results plus a `computing` flag. Classification SHALL run entirely server-side (the merge-safety classifier), never on the calling client. Results SHALL be cached (surviving a daemon restart) so repeat calls do not re-spend the shared GitHub GraphQL budget on already-confirmed-safe tasks.

#### Scenario: Compute starts a background pass
- **WHEN** `POST /api/maintenance/cleanup-candidates/compute` is called and no computation is currently running
- **THEN** the server starts a background classification pass and returns immediately without waiting for it to finish

#### Scenario: Compute is idempotent while running
- **WHEN** `POST /api/maintenance/cleanup-candidates/compute` is called while a computation is already in flight
- **THEN** the server does not start a second concurrent pass

#### Scenario: List reflects cached results and in-flight status
- **WHEN** `GET /api/maintenance/cleanup-candidates` is called
- **THEN** the response includes every currently-eligible task's last cached classification (tier, safe/not-safe, reason) and a `computing` flag reflecting whether a pass is currently running

#### Scenario: Safe verdicts are cached as terminal
- **WHEN** a task's cached verdict is confirmed-safe
- **THEN** a subsequent compute pass does not re-classify it (no repeat GraphQL cost for that task)

#### Scenario: Not-safe verdicts are re-checked on the next compute
- **WHEN** a task's cached verdict is not-confirmed
- **THEN** a subsequent compute pass re-classifies it, since a later merge could change the outcome

### Requirement: Cleanup-candidate clean endpoint is master-only, immediate, and snapshot-scoped

`POST /api/maintenance/cleanup-candidates/clean` SHALL require the master token (an across-all-tasks bulk mutation). It SHALL accept a `scope` of `safe` or `all`, and SHALL act on the currently cached classification snapshot (not a fresh live re-classification), immediately deleting each matching task's row, worktree, and branch via the same guarded deletion primitive the `Ctrl+R` prune-completed flow uses (see the `worktree-management` capability's explicit-ID-list pruning mode) — no intermediate status-flip step, no requirement for a subsequent manual prune. Before deleting each task it SHALL re-verify the task still matches the stuck-task predicate and the live-Hera-binding guard, and SHALL skip (not error) any that no longer do.

#### Scenario: Device token rejected
- **WHEN** a request authenticated as `device` or `scope:<plugin>` calls the clean endpoint
- **THEN** the server responds 403 Forbidden

#### Scenario: Safe-only scope only deletes confirmed-safe tasks
- **WHEN** the master token calls clean with `scope: "safe"`
- **THEN** only tasks whose cached verdict is confirmed-safe are deleted (row, worktree, branch); not-safe tasks are untouched

#### Scenario: All scope deletes every listed candidate
- **WHEN** the master token calls clean with `scope: "all"`
- **THEN** every currently cached candidate (safe and not-safe) is deleted

#### Scenario: A task that no longer qualifies is skipped, not errored
- **WHEN** clean processes a cached candidate whose task has since stopped matching the stuck-task predicate or the live-binding guard
- **THEN** that task is skipped without aborting or erroring the rest of the batch

#### Scenario: Clean acts on the reviewed snapshot, not a fresh classification
- **WHEN** clean runs
- **THEN** it uses the classification results already returned by the most recent `GET /api/maintenance/cleanup-candidates` call, not a newly computed pass

#### Scenario: Clean is immediate, with no separate later step
- **WHEN** clean completes for a scope
- **THEN** the deleted tasks' rows, worktrees, and branches are gone at that point — no further manual action (e.g. a separate prune-completed run) is needed to finish the cleanup

### Requirement: Claude session listing and switching endpoints

The system SHALL expose a `GET /api/tasks/{id}/claude-sessions` endpoint that lists the named task's available Claude sessions (via `internal/claudesession.List`, keyed on the task's worktree) and a `POST /api/tasks/{id}/claude-session` endpoint that switches the task to a different session, mirroring the TUI's `ctrl+r` session-switcher flow (`internal/tui/app.go`'s `openSessionPickerModal`/`switchSession`). Both endpoints SHALL be callable by any authenticated token (master, device, or plugin-scoped) and SHALL be restricted to Claude-backed tasks (matching the TUI's own backend-family session-switcher guard) — a non-Claude-backed task returns 400 rather than an empty or garbage session list.

#### Scenario: List sessions for a Claude-backed task

- **WHEN** a client calls `GET /api/tasks/{id}/claude-sessions` for a Claude-backed task
- **THEN** the response is 200 with `{"sessions": [{"id", "title", "branch", "pr_ref", "mod_time", "size_bytes"}, ...], "current_session_id": "..."}`, newest session first

#### Scenario: List sessions for a non-Claude-backed task

- **WHEN** a client calls `GET /api/tasks/{id}/claude-sessions` for a task whose resolved backend is Codex, Pi, or Opencode
- **THEN** the response is 400 identifying that session listing is Claude-only

#### Scenario: Switch to a different session

- **WHEN** a client posts `{"session_id": "<id>"}` to `POST /api/tasks/{id}/claude-session` for a Claude-backed task, where `<id>` differs from the task's current session
- **THEN** the task's stored session ID is updated to `<id>`, any live session for the task is stopped and restarted (or started fresh if none was running) resuming with `<id>`, and the response is 200 with `{"status": "switched", "pid": <int>}`

#### Scenario: Switch to the already-active session is a no-op

- **WHEN** a client posts the task's current session ID to `POST /api/tasks/{id}/claude-session`
- **THEN** no session is stopped or restarted and the response is 200 with `{"status": "unchanged"}`

#### Scenario: Switch for a non-Claude-backed task or unknown session ID

- **WHEN** a client posts to `POST /api/tasks/{id}/claude-session` for a non-Claude-backed task, or with a missing/empty `session_id`
- **THEN** the response is 400

#### Scenario: Task not found

- **WHEN** a client calls either endpoint for a task ID that does not exist
- **THEN** the response is 404

#### Scenario: Any authenticated token accepted

- **WHEN** a request authenticated with a device token or a plugin-scoped token calls either endpoint
- **THEN** the request is accepted (not rejected as master-only)

### Requirement: Accounts exposed over REST

`POST /api/tasks` (JSON and multipart) SHALL accept an optional `account` (unknown name, or an account that does not support an explicitly requested backend → 400) and task JSON SHALL include `account` and `account_routed`. `GET /api/accounts` SHALL return a bare JSON array, `default` first then configured accounts by name, of `{name, label, claude_config_dir, codex_home, supports: {claude, codex}, is_default, logged_in, email?, org?, plan?, codex_logged_in?, usage?}`. An optional `?project=` SHALL make `is_default` mark that project's resolved default. The `logged_in` / `email` / `org` / `plan` fields describe the Claude login (best-effort, empty for an account without `claude_config_dir`); `codex_logged_in` is omitted when unknown or when the account has no `codex_home`. `usage` SHALL carry the account's currently valid Claude readings as `{five_hour_pct?, five_hour_resets_at?, weekly_pct?, weekly_resets_at?}` and SHALL be omitted when none is valid. Sign-in lookups SHALL run under a short budget and degrade rather than fail. The endpoint SHALL never return credentials. `GET /api/skills` SHALL accept optional `account` and `task` parameters selecting whose Claude config dir to read skills from (unknown account or task → 400; no `claude_config_dir` → `~/.claude`).

#### Scenario: Unknown account on create
- **WHEN** a client POSTs `account: "nope"`
- **THEN** the response is 400 and no task is created

#### Scenario: Account lacking the requested backend's tool
- **WHEN** a client POSTs `backend` naming a codex backend and `account` naming a Claude-only account
- **THEN** the response is 400 and no task is created

#### Scenario: Project-aware default
- **WHEN** a client requests `GET /api/accounts?project=acme` and `project_accounts` maps `acme` to `work`
- **THEN** only `work` has `is_default: true`

#### Scenario: Usage reported
- **WHEN** `default` has a valid reading of 100% for its 5-hour window resetting at 17:40
- **THEN** the `default` entry's `usage.five_hour_pct` is 100 and `usage.five_hour_resets_at` is 17:40

### Requirement: Hera mutation endpoints

The REST API SHALL expose authenticated POST endpoints under `/api/hera` for nuke (orchestrator cascade and role), archive/unarchive, pin/unpin, rename, role-status set, and orchestrator kanban-status set. Nuke endpoints SHALL require the master token. Every endpoint SHALL act on an explicit orchestrator or role id (never a bare task id), SHALL NEVER hard-delete a hera row (nuke stamps `nuked_at`), and SHALL return 404 for an unknown id, 409 on a name conflict, and 400 for an invalid status/kanban value or a kanban change on a nested orchestrator. A nuke SHALL end bindings as `user_deleted`, archive sole-bound tasks, preserve tasks bound outside the subtree, and delegate worktree reclaim to `ReconcileHeraReclaims`. `GET /api/hera/orchestrators/{id}/nuke-preview` SHALL return the counts the nuke would act on without mutating anything.

#### Scenario: Nuke cascades the subtree
- **WHEN** a master-token client POSTs `/api/hera/orchestrators/{id}/nuke` for an orchestrator with a nested sub-orchestrator
- **THEN** both orchestrators and all their roles are stamped nuked and disappear from `GET /api/hera`

#### Scenario: Non-master token rejected
- **WHEN** a non-master token POSTs a nuke endpoint
- **THEN** the response is 403 and nothing changes

#### Scenario: Multi-bound task preserved
- **WHEN** a nuked role's task is also live-bound under an orchestrator outside the subtree
- **THEN** the task, its session and worktree are left untouched; only the role row is nuked

#### Scenario: Hiding a worker stops its session
- **WHEN** a client archives a worker role with a live session
- **THEN** the role is archived and the session stopped; unarchive never touches a session

### Requirement: Settings update validates global extra-write paths

`PUT /api/settings` SHALL respond 400 and persist nothing when any `sandbox.extra_write` entry is `/`, is neither absolute nor `~/`-prefixed, contains a comma, or contains a control character, or is not a clean path (`.`, `..` or `//` segments, or `~/` alone). The same rule SHALL apply to a project's `sandbox.extra_write` on `POST/PUT /api/projects`.

#### Scenario: Rejecting a root grant

- **WHEN** a master-token client sends `{"sandbox":{"extra_write":["/"]}}`
- **THEN** the response is 400 and the stored list is unchanged

### Requirement: Backend listing carries family metadata

`GET /api/backends` SHALL include, per backend, its family `kind` (`claude`, `codex`, `pi`, `opencode`, `antigravity`, or empty for custom), a `label` (the family's human label when the backend is named after its family, otherwise the backend's own name so two instances of one family stay distinguishable), the prompt `skill_prefix` (`$` for codex, `/` otherwise), and `session_switcher` (whether the Claude session endpoints accept it). The create and update responses SHALL carry the same fields for the stored backend. Clients SHALL use these fields instead of hardcoding backend names.

#### Scenario: Second instance keeps its own label
- **WHEN** a backend named `claude-haiku` runs the `claude` command
- **THEN** its kind is `claude` and its label is `claude-haiku`

#### Scenario: Antigravity listed with metadata
- **WHEN** the backend roster contains `antigravity` with command `agy`
- **THEN** its entry has kind `antigravity`, label `Antigravity`, skill_prefix `/`, and session_switcher false

### Requirement: Bridge event ingest endpoint
The REST API SHALL expose `POST /api/tasks/{id}/bridge/events`, authenticated like every `/api/*` route, accepting `{"events":[...]}` and recording them as the task's bridge state. It SHALL return 404 for an unknown task and 400 for a malformed body, and SHALL NOT alter any session or task state.

#### Scenario: Valid batch
- **WHEN** an authenticated client posts a valid batch for an existing task
- **THEN** the response is 200 with the count of accepted events and the task's bridge state reflects them

#### Scenario: Unknown task
- **WHEN** the task id does not exist
- **THEN** the response is 404

#### Scenario: Unauthenticated
- **WHEN** the request carries no valid token
- **THEN** the response is 401

### Requirement: Bridge state and shadow statistics endpoints
The REST API SHALL expose `GET /api/tasks/{id}/bridge` returning the task's latest bridge state (404 when none) and `GET /api/bridge/shadow` returning, per signal (`idle`, `needs_input`, `needs_input_permission`), the counters `agree`, `disagree`, `settling` and `no_bridge`, the settle window, and the currently open disagreements.

#### Scenario: Stats
- **WHEN** a client requests `GET /api/bridge/shadow`
- **THEN** the response lists the signals with their four counters

#### Scenario: Oversize body
- **WHEN** an event batch exceeds 1 MiB
- **THEN** the request is rejected as a bad request and nothing is recorded

### Requirement: Batched bridge authority snapshot

The authenticated GET /api/bridge/states endpoint SHALL return a task-id-keyed map of currently live, launch-matched Claude bridge states with uncertain permission needs-input corroborated against raw scraper input. Local daemon RPC and remote TUI SHALL read the same snapshot semantics. Existing task, Hera and status endpoints SHALL use these same resolved verdicts while preserving live bridge busy authority throughout permission intervals.

#### Scenario: Remote TUI reads bridge state

- **WHEN** the remote TUI refreshes its running tasks
- **THEN** it fetches a batched live snapshot and overrides terminal inference for authoritative bridge sessions

#### Scenario: No live bridge

- **WHEN** no current bridge is live
- **THEN** the batch endpoint returns an empty object and clients use scraping

### Requirement: Authenticated bridge delivery endpoints

The daemon SHALL expose POST /api/tasks/{id}/bridge/deliveries, /deliveries/authorize and /deliveries/ack under the existing authenticated task API. Pull and authorization SHALL require the current generation and boot; acknowledgment SHALL require its unguessable claim token. The daemon SHALL expose POST /api/tasks/{id}/bridge/budget for the coordinator Stop hook to enqueue an eligible nudge after its existing gates, returning enabled=false when the route is unavailable. Transport acknowledgment SHALL not mark durable mail read. These endpoints SHALL remain available to all frontends; web/macOS Bridge settings controls are a named follow-up.

#### Scenario: Disabled bridge delivery cannot authorize

- **WHEN** an authenticated mod requests authorization while bridge.delivery is false
- **THEN** the daemon returns authorized=false and the existing PTY path handles new deliveries

### Requirement: Bridge settings API round-trip

GET and PUT /api/settings SHALL expose a bridge section with enabled and delivery booleans. Partial updates SHALL leave absent keys unchanged and return effective configuration after the standard config.toml overlay. Remote TUI persistence SHALL use this section; web and macOS controls remain a named follow-up.

#### Scenario: Delivery-only update preserves master switch

- **WHEN** an authenticated client updates bridge.delivery alone
- **THEN** the API persists that value and returns the unchanged effective bridge.enabled value alongside it

### Requirement: Read-only bridge UI snapshot

GET /api/tasks/{id}/bridge/ui SHALL require existing API authentication and return effective UI settings, bridge liveness, bindings, role status, unread message summaries and context/coordinator budget. Each coordinator binding SHALL carry `workers` with the count of its orchestrator's active worker roles whose Hera status is working and blocked; cancelled planned nodes and roles with no status SHALL NOT count, and other binding kinds SHALL omit the field. Unknown tasks SHALL return 404. Reads SHALL never acknowledge or mark mail read, claim deliveries or mutate roles.

#### Scenario: Coordinator worker roll-up

- **WHEN** a coordinator's orchestrator has one working worker, two blocked workers, a done worker, a worker with no status and a cancelled planned node marked working
- **THEN** the coordinator binding's `workers` is `{"working": 1, "blocked": 2}` and a worker binding on the same task has no `workers` field

#### Scenario: Snapshot preserves mail

- **WHEN** an authenticated client repeatedly reads the snapshot
- **THEN** unread messages and pending deliveries remain unchanged

### Requirement: Bridge UI settings API

GET and PUT /api/settings SHALL include band and toasts booleans in the bridge section. Partial updates SHALL preserve omitted settings and return effective config.toml-over-DB values. Remote TUI SHALL map both keys through this contract.

#### Scenario: Band-only update

- **WHEN** a client updates bridge.band alone
- **THEN** the API persists it and returns unchanged effective delivery, enabled and toasts values

### Requirement: Move a task to another account over REST

`POST /api/tasks/{id}/account` with JSON `{"account": "<name>|default|auto", "force": <bool, optional>}` SHALL perform the account-move operation and resume the task's session as `POST /api/tasks/{id}/restart` does, responding 200 with `{from, to, resumed, started}`. An unknown task SHALL return 404; a live, non-idle session SHALL return 409 unless `force` is true, as SHALL a move or restart already in progress; every other rejection (unknown or unsupported target, same account, non-Claude backend, no other candidate for `auto`) SHALL return 400 with the reason. A failure to restart after the move has persisted SHALL return 500 with the task left on the target account.

#### Scenario: Move succeeds
- **WHEN** a client POSTs `{"account": "personal"}` for an idle task on `default`
- **THEN** the response is 200 with `from: "default"`, `to: "personal"`, and the task runs on `personal`

#### Scenario: Busy task
- **WHEN** the task's session is actively producing output
- **THEN** the response is 409 and nothing changes

### Requirement: Sandbox proposals are readable by any token and decidable only by the master token

The API SHALL serve `GET /api/sandbox/proposals` (optional `status` filter) and `GET /api/sandbox/proposals/{id}` to any authenticated token. `POST /api/sandbox/proposals/{id}/approve` (body: `scope` project|global, optional edited `paths`) and `POST /api/sandbox/proposals/{id}/reject` SHALL require the master token and return 403 otherwise. Deciding an already-decided proposal SHALL return 409.

#### Scenario: Device token cannot approve

- **WHEN** a device-token request POSTs approve
- **THEN** the response is 403 and the proposal remains pending

#### Scenario: argus_api_get can read status

- **WHEN** an agent reads `/api/sandbox/proposals` through `argus_api_get`
- **THEN** it receives the proposal list including each status

#### Scenario: Double approve

- **WHEN** the master token approves an already-approved proposal
- **THEN** the response is 409 and config is unchanged

### Requirement: Task context endpoint
`GET /api/tasks/{id}/context` SHALL return `{task_id, session_id, provider, live, measured_at, source, context}` where `context` is `{used_tokens, window_tokens, percent, categories}` and `categories` always lists `messages`, `memory`, `tools`, `agents`, `skills`, `system`, `compaction` with `null` for unmeasured values. It SHALL return 404 for a missing task and 200 with `live=false` and `context=null` for a task without a measurement or whose recorded measurement belongs to a replaced launch. The `live` flag SHALL reflect current process authority and bridge health, not measurement age. Unchanged context is deduplicated, so an old measurement with fresh heartbeats from the current launch SHALL remain `live=true`. Clients MAY use `measured_at` to display age. Replacing the session, process generation or account launch SHALL invalidate the old context. When the bridge is no longer live without a known replacement, recorded data SHALL return with `live=false`. It SHALL require authentication and expose aggregate counts only. After a live measurement the daemon SHALL emit `task.context_changed` with `{session_id, measured_at}` and no counts. For a Codex task the daemon SHALL read the recorded session's rollout (`provider` `openai`, `source` `codex_rollout`), report the latest `last_token_usage.total_tokens` (including compaction estimates) with `categories` all `null`, and leave window and percent `null` when no window was recorded. An absent, null, negative or malformed last total SHALL be unmeasured rather than manufactured zero. The read SHALL be bounded and read-only and SHALL NOT launch a Codex CLI or model turn. The daemon SHALL emit `task.context_changed` when a running reader-backed task's measurement time advances, with the same identity-only payload. Unpinned selection SHALL exclude subagents and reject conversations created before the current process launch. Rollouts SHALL be regular files in the task account's source-home or exact derived Argus overlay session storage and their session metadata SHALL match the selected session ID and worktree.

#### Scenario: Live measurement
- **WHEN** the current launch reports a measurement
- **THEN** GET returns `live=true` with its counts and one `task.context_changed` event was emitted

#### Scenario: Replaced session
- **WHEN** the running process's generation differs from the recorded measurement's
- **THEN** GET returns `live=false` and `context=null`

#### Scenario: Unchanged context remains live with fresh heartbeats
- **WHEN** a ten-minute-old context measurement belongs to the current launch and a fresh heartbeat arrives
- **THEN** GET returns `live=true` with the original counts and original `measured_at`

#### Scenario: Account replacement invalidates old context
- **WHEN** a task moves from account A to account B with a replacement process generation
- **THEN** GET hides A's context until B reports its own measurement, then returns B's session identity and category counts

#### Scenario: Codex measured context
- **WHEN** a Codex task's recorded rollout holds a token_count event with a window
- **THEN** GET returns `provider=openai`, `source=codex_rollout`, `used_tokens` from the last call, the window and a derived percent

#### Scenario: Codex session replaced or unmeasured
- **WHEN** the task's recorded session id has no thread in its worktree, or the rollout has no usable event
- **THEN** GET returns 200 with `context=null`

#### Scenario: Codex compaction estimate
- **WHEN** the latest last-token usage has zero input and output with `total_tokens=200000`
- **THEN** GET reports `used_tokens=200000`, matching Codex's context meter

Codex meter semantics are pinned to [upstream TokenUsage and TokenUsageInfo](https://github.com/openai/codex/blob/748ab55ca7de0b647d9a7a9d9a8e70e00f28666d/codex-rs/protocol/src/protocol.rs#L2493-L2625) and [context history](https://github.com/openai/codex/blob/748ab55ca7de0b647d9a7a9d9a8e70e00f28666d/codex-rs/core/src/context_manager/history.rs#L1143-L1158).

### Requirement: Bridge state carries the suggested next prompt
`GET /api/tasks/{id}/bridge` SHALL include `suggestion`, either `null` or `{text, session_id, generation, recorded_at}`. It SHALL be `null` when the provider offered none, the provider has no suggestion support, the suggestion was cleared or replaced, or the bridge is not live authority for the running launch. The existing 404 without bridge state SHALL be preserved. When the stored suggestion appears, changes or clears the daemon SHALL emit `task.suggestion_changed` with `{session_id, available, recorded_at}` and no text.

#### Scenario: Current suggestion
- **WHEN** the current launch reports a suggestion
- **THEN** GET returns it with session identity and timestamp and one `task.suggestion_changed` event with `available=true` was emitted

#### Scenario: Lost authority
- **WHEN** the running process's generation differs from the recorded one
- **THEN** GET returns `suggestion=null` with `live=false`

### Requirement: Read-only Hera message history

The system SHALL expose `GET /api/tasks/{id}/hera-messages` returning mail addressed to every role the task is or was bound to, paged by message id (`limit` 1-500 default 100, exclusive `before_id`), oldest first within a page, with `next_before_id`. It SHALL NOT mark messages read, alter delivery state or advance role tree cursors. Store failures SHALL return HTTP 500 with a generic error message; detailed diagnostics SHALL be logged on the server.

#### Scenario: History does not consume mail

- **WHEN** a client reads the history
- **THEN** no `read_at`, delivery state or unread count changes

#### Scenario: Invalid cursor

- **WHEN** `limit` or `before_id` is invalid
- **THEN** the response is 400

#### Scenario: History store failure

- **WHEN** task lookup or history retrieval fails because the store is unavailable
- **THEN** the response is 500 with `internal server error` and the store diagnostic appears only in server logs

### Requirement: Human board read-state and dismissal

The system SHALL expose `PUT /api/hera/roles/{role_id}/board/{item_id}/read-state` and `POST .../dismiss`, each requiring `expected_updated_at`, returning `{item, unread_count}`, and failing with coded errors `invalid_request`, `board_item_not_found`, `revision_conflict`, `item_resolved` or `board_write_failed`. Dismissal notes SHALL allow up to 64 KiB of decoded UTF-8 bytes, including JSON-escaped content, while the encoded request body remains bounded. A read-state conditional UPDATE that affects no row SHALL return the current missing, resolved or revision-conflicted error rather than success, and SHALL NOT emit a success event. REST and strict store mutations SHALL reject zero expected revision timestamps. Existing legacy resolution without an expected revision SHALL remain unchanged. Both REST handlers SHALL require exactly one JSON value and consume the bounded body through EOF before any mutation, including trailing whitespace in the encoded allowance.

#### Scenario: Stale revision

- **WHEN** the item changed since `expected_updated_at`
- **THEN** the response is 409 `revision_conflict` and the item stays unread and open

#### Scenario: Repeated dismissal

- **WHEN** an identical dismissal is repeated
- **THEN** the resolved item is returned and no second coordinator notice is written

#### Scenario: Escaped note at the limit

- **WHEN** a dismissal contains a valid note of at most 64 KiB decoded bytes, even when JSON escaping expands its encoded size
- **THEN** the dismissal succeeds and preserves the complete note

#### Scenario: Oversized dismissal

- **WHEN** the decoded note exceeds 64 KiB or the encoded request exceeds its bounded transport allowance
- **THEN** the response is 400 and the item remains unchanged

#### Scenario: Independent writer edits during acknowledgment

- **WHEN** another connection commits a revision change between read-state validation and its conditional UPDATE
- **THEN** the response is 409 `revision_conflict`, the edited item stays unread and no read-state success event is emitted

#### Scenario: Independent writer resolves or deletes during acknowledgment

- **WHEN** another connection resolves or deletes the item between read-state validation and its conditional UPDATE
- **THEN** the response is 409 `item_resolved` or 404 `board_item_not_found` respectively, with no successful item/count response or read-state event

#### Scenario: Zero expected revision

- **WHEN** either REST mutation or strict store mutation receives a zero expected revision timestamp
- **THEN** it fails as invalid input, REST returns 400 `invalid_request`, and the item, notices and events remain unchanged

#### Scenario: Trailing invalid input

- **WHEN** a valid board mutation object is followed by garbage or another JSON value
- **THEN** the response is 400 `invalid_request` and no item, notice or event is changed

#### Scenario: Trailing whitespace exceeds allowance

- **WHEN** trailing whitespace takes the entire encoded mutation body above its route's allowance
- **THEN** the response is 400 `invalid_request` before mutation, while whitespace within the allowance remains valid

### Requirement: Board change event

The system SHALL emit `hera.board_changed` after each committed board post, material edit, read-state change or resolution in the daemon process, with payload `{role_id, item_id, unread_count, operation}` and no item bodies. Forwarding TUI-originated events across the process boundary to daemon SSE is deferred to ENG-1125; those writes remain durable and clients can refetch the roster for `board_unread`.

#### Scenario: Event follows commit

- **WHEN** a board mutation commits
- **THEN** one event is emitted with the updated unread count

#### Scenario: Reopening an acknowledged item

- **WHEN** a legacy reader acknowledges the same open board item more than once without an intervening unread transition
- **THEN** only the first unread to read transition emits an event

### Requirement: Recipient-indexed Hera history

History paging SHALL use a full recipient/message-id index covering both read and unread messages, with separate first-page and cursor queries so a cursor supplies a direct exclusive id range. Historical and current bindings SHALL retain the same message selection and ordering.

#### Scenario: History with unrelated read mail

- **WHEN** a task's ended and active roles have matching mail amid substantial unrelated read history
- **THEN** each page seeks the matching recipients, cursor pages also seek `id < before_id`, and returned IDs remain oldest first within the selected newest page

### Requirement: Targeted task pin action

The system SHALL expose `POST /api/tasks/{id}/pin` accepting `{"pinned":<bool>}` and returning 200 `{"task_id","pinned","archived"}` reflecting the stored flags. It SHALL write only the pinned and archived columns using SetPinned semantics (pinning clears archived; unpinning leaves archived alone) and SHALL NOT overwrite any other task field or start or stop a session. A request that changes nothing SHALL succeed without emitting `task.updated`. The endpoint SHALL return 400 for malformed JSON, a missing, null or non-boolean `pinned`, unknown fields, trailing non-whitespace, multiple JSON values, or a body over 1 KiB (including trailing whitespace); 404 for an unknown task; 500 for a store failure; and SHALL be open to any authenticated token.

#### Scenario: Pin preserves concurrent edits

- **WHEN** a task is renamed and its status changed, then pinned via the endpoint
- **THEN** the stored name and status are the newer values and only pinned changed

#### Scenario: Pin restores an archived task

- **WHEN** an archived task is pinned
- **THEN** the response has `pinned:true, archived:false` and the stored row matches

#### Scenario: Invalid body

- **WHEN** the body is `{"pinned":"yes"}` or `{}` or contains an unknown field
- **THEN** the response is 400 and nothing is stored or emitted

#### Scenario: Complete pin request validation

- **WHEN** a valid pin object is followed by garbage, another JSON value, or whitespace that takes the complete body above 1 KiB
- **THEN** the response is 400 and no task field or event is changed

#### Scenario: Pin body at the limit

- **WHEN** exactly one valid pin object and optional whitespace occupy at most 1 KiB through EOF
- **THEN** the request succeeds with the stored flags

### Requirement: Resize reports client-side keep-place state

`POST /api/tasks/{id}/resize` SHALL accept optional boolean `scrolled_up` and `composer_dirty` fields (absent means false) and SHALL return a `deferred` field: `"scrolled"`, `"draft"` or empty. When the resize would trigger the size-drift kill + resume, the daemon SHALL defer it when the composer holds unsent text (the Claude mod's report, else the rendered Claude composer read from the session's output tail when it is found, else `composer_dirty`) and otherwise when `scrolled_up` is true; the draft gate outranks the scroll gate. A deferral SHALL keep the task's cols cache invalidated so a later same-size resize re-evaluates, and SHALL be logged. `composer_dirty` alone SHALL stop holding after 10 minutes of deferrals at one width; the scroll gate is uncapped. The rendered-composer read SHALL run only while a kick is being evaluated. With `ui.rerender_keep_place` off both fields SHALL be ignored.

#### Scenario: Scrolled-up client

- **WHEN** a resize crosses the rerender margin with `scrolled_up` true and an empty composer
- **THEN** no kick is queued, the response carries `deferred: "scrolled"`, and a later same-size resize with `scrolled_up` false kicks

#### Scenario: Client flag only

- **WHEN** no mod report exists, the composer is not found on screen, and `composer_dirty` is true
- **THEN** the kick is deferred with `deferred: "draft"` until the flag clears or 10 minutes pass at that width

#### Scenario: Screen overrides a stale flag

- **WHEN** the rendered Claude composer is found empty and `composer_dirty` is true
- **THEN** the flag does not hold the kick

#### Scenario: Old client

- **WHEN** a resize omits both fields
- **THEN** behavior is unchanged from before this requirement

#### Scenario: Setting off

- **WHEN** `ui.rerender_keep_place` is off
- **THEN** both fields are ignored and no screen read occurs

#### Scenario: Steady state

- **WHEN** a resize does not cross the margin, or the cols are unchanged
- **THEN** the rendered-composer read does not run

