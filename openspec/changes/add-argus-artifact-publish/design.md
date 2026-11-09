## Context

`add-artifact-home-account` (merged, #1194) resolves each project's artifact home account. It exports `ARGUS_ARTIFACT_ACCOUNT` when an agent can't publish there with Claude Code's built-in tool, and tells the agent to use an `artpush` CLI that doesn't exist. This change replaces that CLI with publishing inside the Argus daemon.

## Discovery findings (spike, 2026-11-09)

- **Endpoint.** Claude Code's built-in `claude-artifacts` server is an MCP streamable-HTTP endpoint at `https://api.anthropic.com/api/artifacts/mcp`. It needs only these headers:
  - `Authorization: Bearer <token>`
  - `anthropic-beta: oauth-2025-04-20`
  - `Accept: application/json, text/event-stream`
  - `Content-Type: application/json`

  No organization or account ID is sent.
- **Stateless.** `initialize` returns 404. Bare `tools/list` and `tools/call` POSTs work.
- **Token scope.** A `claude setup-token` token has scope `user:inference` and a one-year expiry.
  - On the personal account it got the full 41-tool list. `list_artifacts` returned real artifacts, and `publish_version` created a private artifact (HTTP 200, result JSON in `content[0].text` with `slug`, `url`, `version`).
  - No model run was involved.
- **Exhausted account.** On the work account, which is at 0% budget, the same kind of token got a 5-tool list. Every `tools/call`, including `list_artifacts`, returned **HTTP 404** with the text "not found: the artifact tools on this server aren't available to this session…".
  - Org policy (`/api/claude_code/policy_limits`) is the same on both accounts, so budget exhaustion is the likely cause.
  - Confirm by re-running the probe after the work account resets.
- **Tool list isn't a reliable signal.** The exhausted account's cut-down tool list still includes `publish_version`. Only a real call shows whether an account is available.

## Goals / Non-Goals

**Goals:**

- An agent routed off its project's home account can publish a single-page artifact to the home account with one tool call, and get the link back.
- Tokens never reach agents: not in env, tool results, logs or REST responses.
- An exhausted home account never silently loses a page. The user picks what happens.

**Non-Goals:**

- Multi-file artifacts, images and assets (`publish_files` / `authorize_upload`). Named follow-up.
- Automatic retry after a budget reset. Named follow-up.
- Publish buttons in the TUI, web or macOS clients. Named follow-up; the REST endpoint is the parity surface.
- Comments, watching and the artifact database.
- Any interactive OAuth login. The user runs `claude setup-token` themselves.

## Decisions

### D1. Credential: one `claude setup-token` per account, referenced from config

`[artifact_tokens]` maps an account name to a secret source string, resolved with the existing secrets resolver (`agent.Resolve(cfg.Secrets, source)`, which supports `op://` and `keychain://`) at publish time, inside the daemon. A literal token in config is rejected: the value must be a secret reference.

Alternatives considered:

- **Argus's own browser login.** Rejected: there is no public OAuth client, so Argus would have to present itself as Claude Code to Anthropic, with a terms-of-service risk.
- **Borrowing Claude Code's stored login.** Rejected: refreshing it can sign the user out of Claude Code.

### D2. Publishing runs in the daemon; agents and people reach it through MCP and REST

- The MCP server and REST API already run in the daemon, outside the agent sandbox. Agents call `artifact_publish`, and people call `argus artifact publish`, which uses `POST /api/artifacts/publish`.
- Both go through one `claudeartifacts.Client`.
- This keeps tokens out of agent processes. It also lets non-Claude agents (Codex, Pi, opencode), which get Argus's MCP tools, publish too.

### D3. Single-page HTML via `publish_version`

- **Create:** `publish_version {title, content, icon:"page"}`.
- **Update:** parse the slug from a `claude.ai/artifact/…` or `claude.ai/code/artifact/…` link, call `get_artifact {slug}` for `version`, then `publish_version {slug, base_version, content}`, leaving the title unchanged unless one is given.
- **Input limits:** the file must be `.html` or `.htm`, at most 16 MiB, and start with `<!DOCTYPE html>` (case-insensitive) or `<html`. Anything else is rejected with a clear error.
- **Results:** taken from `content[0].text` (JSON). `structuredContent` is preferred when present.

### D4. Unavailable account detection and fallback

- **Detection:** HTTP 404 on a `tools/call` with a body containing "aren't available to this session" ⇒ `ErrAccountUnavailable`. Other non-2xx responses ⇒ a general error that includes the status and the first 300 bytes of the body.
- **On unavailable,** `artifact_publish` does three things:
  1. It registers the page as a task artifact through the existing `artifact_register` code path, so it lives in the task's artifact directory and shows up in Argus.
  2. It probes every other account in `[artifact_tokens]` with `list_artifacts {limit:1}`, concurrently, with a 15 s timeout each. An account is available if the call returns 200.
  3. It returns a result (not a tool error) with `status:"unavailable"`, the account, the local artifact filename, and the list of available accounts.
- **From the CLI,** the file is already local, so nothing is registered. The CLI exits with code 3 and prints the available accounts.

### D5. `artifact_open`

- Parameters: `filename` (a registered artifact of the calling task) and `reveal` (bool).
- The daemon runs `open <path>`, or `open -R <path>` to reveal, on macOS only. Elsewhere it returns a clear "not supported on this platform" error, following the os-integration platform gating.
- Only manifest-registered files can be opened, with the same path-escape defense as serving.

### D6. Routing guidance: tool name and the four-option question

The snippet tells the agent:

- When `ARGUS_ARTIFACT_ACCOUNT` is set, call `artifact_publish` with `account: $ARGUS_ARTIFACT_ACCOUNT`.
- On `status:"unavailable"`, ask the user with its question tool, offering:
  1. publish to an available account (one option per available account, which re-calls `artifact_publish` with that account)
  2. open in the browser (`artifact_open`)
  3. reveal in Finder (`artifact_open reveal:true`)
  4. park it for later (do nothing more, and tell the user the local copy name and the `argus artifact publish <file> --account <home>` command)
- If there are no available accounts, option 1 is left out.
- Never fall back to the built-in tool on its own initiative.

### D7. Identifying the calling task

`artifact_publish` and `artifact_open` resolve the calling task the same way `artifact_register` does. When no task can be resolved, `artifact_publish` still publishes, but skips local registration on an unavailable account, and `artifact_open` errors.

## Risks / Trade-offs

- **Undocumented endpoint changes shape** → every non-success is a loud error with the status and a body excerpt. No silent fallback. A unit test fixture pins the request and response shape.
- **Work-account block has another cause** (plan type, not budget) → the spike's re-probe after the reset settles it. If the plan is the cause, work publishing never works with setup-tokens, and the four-option question still covers it.
- **Token leakage** → the token lives only inside the client's request. Tests assert it is absent from tool results, logs, REST bodies and errors.
- **Setup-token expiry (one year)** → a 401 maps to an error that says to re-run `claude setup-token` for that account and update the 1Password entry.
- **Probing costs** → one `list_artifacts` per other account, only on the unavailable path. That's negligible.

## Migration Plan

1. Merge.
2. Add `[artifact_tokens]`.
3. Restart the daemon (the MCP and REST changes live there).

The routing snippet change reaches new agent sessions. No data migration is needed. Rollback: remove `[artifact_tokens]`, and `artifact_publish` returns "no token configured for account X".

## Open Questions

- Confirm that the work account's refusal is caused by budget, by re-running the probe after its reset. This doesn't block implementation.

## Acceptance criteria

**Tokens**

- It should resolve the account's token from `[artifact_tokens]` through the secrets resolver at publish time.
- It should fail with a message naming `[artifact_tokens]` when the account has no entry.
- It should never include the token in tool results, logs, REST responses or errors.

**Publish**

- It should publish a single HTML file and return its claude.ai link.
- It should update an existing artifact given its link, publishing a new version on top of the current one.
- It should reject a file that is not HTML or exceeds 16 MiB, before any network call.
- It should map HTTP 401 to an error telling the user to re-run `claude setup-token` for that account.

**Unavailable account**

- It should detect the "not available to this session" refusal as account unavailable.
- It should register the page as a task artifact and report its filename.
- It should report which other configured accounts can publish, and exclude ones that are also unavailable.

**Open**

- It should open a registered artifact in the default app, or reveal it in Finder.
- It should refuse a filename that is not a registered artifact of the calling task.

**CLI and REST**

- `argus artifact publish` should print the link on success.
- It should exit 3 and list available accounts when the account is unavailable.

**Routing guidance**

- The snippet should name `artifact_publish` with the home account.
- It should describe the four-option question for an unavailable account.
- It should say never to fall back on its own to the built-in tool.
