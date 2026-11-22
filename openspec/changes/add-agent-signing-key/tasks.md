**Design doc:** `openspec/changes/add-agent-signing-key/design.md`

All tests use synthetic Ed25519 keys generated in-test and a stubbed secret resolver. They never touch real 1Password, `~/.argus`, or the live daemon.

## 1. Failing tests

- [x] 1.1 `internal/config`: validation tests for `agent-key` (accepted with an `op://` key; rejected when the key is missing or non-`op://`; the invalid-value message names all three values)
- [x] 1.2 `internal/agent`: `agentGitPolicy` tests for `agent-key` (four GIT_CONFIG entries appended after existing entries, `GIT_CONFIG_PARAMETERS` appended when inherited, no private key material in the env, `inherit`/`unsigned` output unchanged)
- [x] 1.3 Signing service tests: SSHSIG output verifies with the real `ssh-keygen -Y verify` (skip if `ssh-keygen` is absent); non-`git` namespace refused; mismatched halves refused; failed resolution retried on the next request; log lines carry task ID and outcome and no data
- [x] 1.4 `POST /api/git/sign` route tests: auth required, policy-off answers a named error, success path
- [x] 1.5 Helper tests (`cmd/argus`): `-Y sign` writes `<file>.sig` from a fake daemon; non-sign `-Y` passes through to a stub `ssh-keygen`; connection retry succeeds inside 30s; an error response is final with a non-zero exit and a "report to your coordinator" message; never produces an unsigned outcome
- [x] 1.6 Doctor tests: `OFF` / `OK` / `BROKEN: <cause>`, exit code unaffected, no key material printed
- [x] 1.7 Confirm every test above fails before implementation (Prove-It)

## 2. Config and session environment

**Depends on:** Stage 1

- [x] 2.1 Add `agent-key` and `AgentSigningKey` to `config.GitConfig` with validation
- [x] 2.2 Extend `agentGitPolicy` to take the resolved public key and the argus executable path, and emit the four entries
- [x] 2.3 In `BuildCmd`, resolve `<ref>/public key` through `resolveBackendEnvSource`; on failure still point `gpg.ssh.program` at the helper so commits fail loudly; extend the `[agent-git]` log line
- [x] 2.4 Re-record the supervisor spawn-surface digest (`gotchas/ci-gates.md` recipe)

## 3. Daemon signing service

**Depends on:** Stage 1

- [x] 3.1 Add the in-process SSHSIG signer (`golang.org/x/crypto/ssh`, sha512, armored output)
- [x] 3.2 Add the daemon key holder: lazy success-only memoized resolve of `?ssh-format=openssh` private key, public-half match check, named errors
- [x] 3.3 Wire `POST /api/git/sign` in `internal/api/routes.go` with the daemon's existing auth, the `git`-only namespace guard, and `[agent-git] sign task=… ok|err=…` logging

## 4. Signing helper

**Depends on:** Stage 3

- [ ] 4.0 Wire the signer into the daemon (moved from Stage 3; needs Stage 2's config field): when `[git] agent_commit_signing = "agent-key"`, build a `gitsign.KeyHolder` from `agent_signing_key` with the daemon's secret resolver and call `Server.SetGitSigner` at startup
- [ ] 4.1 Dispatch `os.Args[1] == "-Y"` in `cmd/argus/main.go` to the helper before any other startup work
- [ ] 4.2 Implement `-Y sign` (daemon discovery and token reuse from `artifact.go`, 30s connection retry, `.sig` write, fail-loud message) and the `ssh-keygen` passthrough

## 5. Doctor check and docs

**Depends on:** Stage 3

- [ ] 5.1 Add the advisory agent-signing-key line to `argus doctor`
- [ ] 5.2 README Reference: `[git]` table rows for `agent-key` and `agent_signing_key`, plus setup steps (1Password SSH Key item in `claude`, GitHub Signing Key registration, `argus doctor`, restart or recycle sessions)
- [ ] 5.3 Gotcha bullets in `context/knowledge/gotchas/misc.md` (`gpg.ssh.program` is exec'd without a shell, hence the `-Y` dispatch; `?ssh-format=openssh` is required for the private key; spawn-surface re-record; signing depends on the daemon)

## 6. Verify, smoke test, archive

**Depends on:** Stages 2, 4, 5

- [ ] 6.1 `make pre-pr` green
- [ ] 6.2 Live smoke test on Aaron's machine after he creates the key: deploy, set `agent-key`, `argus doctor` reports `OK`, a fresh sandboxed agent session commits with no prompt, `git log --show-signature` verifies, and GitHub shows Verified on the PR commits
- [ ] 6.3 `openspec archive add-agent-signing-key --yes` and `openspec validate --all --strict`; commit the archive in the PR
