**Design doc:** `openspec/changes/add-argus-artifact-publish/design.md`

## 1. Tests

- [ ] 1.1 `internal/claudeartifacts`: `httptest` server tests for the client:
  - request shape (URL path, the four headers, JSON-RPC `tools/call` body)
  - create returns the link
  - update calls `get_artifact` then `publish_version` with `slug` + `base_version`
  - the "aren't available to this session" 404 maps to `ErrAccountUnavailable`
  - 401 maps to the re-run-setup-token error
  - other errors include status and a body excerpt
  - SSE-framed responses parse
  - the token is never present in returned errors
- [ ] 1.2 `internal/claudeartifacts`: input validation table (non-HTML extension, missing doctype/`<html`, >16 MiB) rejected with no request made
- [ ] 1.3 `internal/config`: `[artifact_tokens]` decodes and merges; a non-reference value is rejected
- [ ] 1.4 `internal/mcp`: `artifact_publish` and `artifact_open` tests:
  - success result
  - no-token error
  - unavailable path registers the file as a task artifact and lists only accounts whose probe succeeds
  - no-task unavailable path registers nothing
  - token absent from results and logs
  - `artifact_open` refuses unregistered names and builds `open` / `open -R` through an injectable opener seam
- [ ] 1.5 `internal/api`: `POST /api/artifacts/publish` success, unavailable and auth tests
- [ ] 1.6 `cmd/argus`: `artifact publish` subcommand exit codes (0 success, 3 unavailable, non-zero otherwise) against a stub server, if the cmd package's test conventions allow it; otherwise cover the arg parsing and exit-code mapping in a testable helper
- [ ] 1.7 `internal/routing`: snippet names `artifact_publish`, `ARGUS_ARTIFACT_ACCOUNT`, the update form, all four unavailable options, `artifact_open`, `argus artifact publish`, the no-fallback rule and the self-gate; no `artpush` anywhere
- [ ] 1.8 Confirm every new test fails for the expected reason (Prove-It)

## 2. Publisher client and config

**Depends on:** Stage 1

- [ ] 2.1 Add `ArtifactTokens map[string]string \`toml:"artifact_tokens"\`` to `Config` and clone it in the `file.go` merge
- [ ] 2.2 Implement `internal/claudeartifacts`:
  - `Client{HTTP, BaseURL}` with `Publish(ctx, token, PublishInput) (Result, error)` and `Probe(ctx, token) error`
  - slug parsing for `claude.ai/artifact/…` and `claude.ai/code/artifact/…` links
  - input validation and error mapping per design D3/D4
- [ ] 2.3 A token-resolution helper in the daemon layer using `agent.Resolve(cfg.Secrets, source)`
- [ ] 2.4 Tests 1.1–1.3 pass

## 3. MCP tools

**Depends on:** Stage 2

- [ ] 3.1 `artifact_publish` in `internal/mcp/artifacts.go`, registered alongside `artifact_register`, resolving the calling task the same way. The unavailable path reuses the `artifact_register` copy+manifest code and runs concurrent 15 s probes.
- [ ] 3.2 `artifact_open` with a manifest + path-escape check and an injectable opener (`open` / `open -R`, macOS-only gate)
- [ ] 3.3 `[artifacts]`-prefixed log lines for every publish outcome, with no token
- [ ] 3.4 Test 1.4 passes

## 4. REST and CLI

**Depends on:** Stage 2

- [ ] 4.1 `POST /api/artifacts/publish` in `internal/api`: multipart or JSON body with the HTML content, title, account and update link, using the same auth as the other endpoints
- [ ] 4.2 `argus artifact publish` subcommand in `cmd/argus` calling the REST endpoint with the local daemon's token; exit codes per spec
- [ ] 4.3 Tests 1.5–1.6 pass

## 5. Routing guidance and artpush removal

**Depends on:** Stage 1

- [ ] 5.1 Rewrite `internal/routing/builtin/argus-artifacts.md` per design D6
- [ ] 5.2 Replace every `artpush` mention: code comments in `internal/agent` and `internal/daemon/surface.go`, `context/knowledge/backends.md`, `gotchas/misc.md`, `index.md` and `README.md`. Confirm whether the spawn surface digest changes (comments only, so it should not).
- [ ] 5.3 Test 1.7 passes

## 6. Docs and gate

**Depends on:** Stages 3, 4, 5

- [ ] 6.1 README Reference: `artifact_publish` and `artifact_open` in the MCP tools table, `POST /api/artifacts/publish` in REST endpoints, `[artifact_tokens]` in config, the `argus artifact publish` subcommand, and the setup-token setup steps
- [ ] 6.2 `gotchas/misc.md` bullets:
  - stateless endpoint (no `initialize`)
  - the cut-down tool list still contains `publish_version`, so probe with a real call
  - the "not available" 404 means unavailable
  - setup-token is `user:inference`-only but works
  - tokens are daemon-only

  Update the index row.
- [ ] 6.3 `make pre-pr` passes clean
- [ ] 6.4 Archive the change on the branch before merge
