**Design doc:** `openspec/changes/add-artifact-home-account/design.md`

**External dependency:** the `artpush` CLI (separate repo and change). It must confirm, before rollout, that publishing works with zero model budget and that its own per-account login works. Stages 1–4 do not need it installed.

## 1. Tests

- [ ] 1.1 `internal/config`: table test for `ArtifactAccount(project)` covering explicit entry wins, fallback to `project_accounts`, unmapped stays empty despite `default_account`, unknown entry skipped with fallback (scenarios in `specs/artifact-home-account/spec.md` › Project artifact home account resolution)
- [ ] 1.2 `internal/config`: config.toml override test that `[project_artifact_accounts]` decodes and merges the same way `[project_accounts]` does
- [ ] 1.3 `internal/agent`: `BuildCmd` table test for the five export scenarios (home match, routed away, Codex in mapped project, unmapped, move back home), asserting the env var is absent rather than empty when not exported
- [ ] 1.4 `internal/agent`: registry test that `NativeArtifacts` is true for `claude` only
- [ ] 1.5 `internal/routing`: test that the injected builtin content includes the `argus-artifacts` snippet with the `artpush publish`, `--account "$ARGUS_ARTIFACT_ACCOUNT"`, `--update`, no-fallback and self-gate text
- [ ] 1.6 Run the new tests and confirm every one fails for the expected reason (Prove-It)

## 2. Home account resolution

**Depends on:** Stage 1

- [ ] 2.1 Add `ProjectArtifactAccounts map[string]string \`toml:"project_artifact_accounts"\`` to `Config`, clone it in the file merge (`file.go`) like `ProjectAccounts`
- [ ] 2.2 Implement `Config.ArtifactAccount(project string) string` per design D2, with `uxlog`/slog line `[artifacts] skipping unknown artifact account ...` on an invalid entry
- [ ] 2.3 Tests 1.1 and 1.2 pass

## 3. Spawn export

**Depends on:** Stage 2

- [ ] 3.1 Add `NativeArtifacts bool` to `agent.Family` in `internal/agent/backends.go`; set true for the claude family only; update `context/knowledge/backends.md` matrix
- [ ] 3.2 In `BuildCmd` (`agent.go`, next to the `ARGUS_TASK_ID` export), compute the home account via `cfg.ArtifactAccount(task.Project)` and export `ARGUS_ARTIFACT_ACCOUNT` per design D3, comparing against the task's effective account (`task.Account` resolved to `default` when empty)
- [ ] 3.3 Re-record the supervisor spawn surface digest (see `gotchas/ci-gates.md`)
- [ ] 3.4 Tests 1.3 and 1.4 pass

## 4. Routing guidance

**Depends on:** Stage 1

- [ ] 4.1 Add `internal/routing/builtin/argus-artifacts.md` (tags `[argus]`, audience `[shared]`, self-gating on `ARGUS_ARTIFACT_ACCOUNT`), text per design D5
- [ ] 4.2 Re-record any routing content digest the routing tests pin
- [ ] 4.3 Test 1.5 passes

## 5. Docs and gate

**Depends on:** Stages 3, 4

- [ ] 5.1 Add a `context/knowledge/gotchas/misc.md` bullet: artifact home account is separate from routing; the export is conditional and never empty; there is no fallback to the built-in tool when artpush fails; config.toml only for now, with Settings UI as a named follow-up. Update the index row.
- [ ] 5.2 Add `[project_artifact_accounts]` to the README Reference config table and `ARGUS_ARTIFACT_ACCOUNT` to the exported-env table
- [ ] 5.3 `make pre-pr` passes clean
- [ ] 5.4 Archive the change (`openspec archive add-artifact-home-account`) on the branch before merge
