## Context

Aaron wants every commit signed, and he does not want repeated Touch ID prompts. Today those goals conflict for agent sessions:

- His global Git config signs every commit through 1Password's SSH agent (`gpg.format=ssh`, `gpg.ssh.program=op-ssh-sign`). That agent asks for approval with Touch ID, and it stops signing entirely while 1Password is locked (auto-lock is 60 minutes on his machine).
- The 2026-10-26 change `enable-unattended-git` added `[git] agent_commit_signing` with `inherit` (sign with the personal key, which prompts) and `unsigned` (agent commits carry no signature). Aaron runs `unsigned`. Only GitHub's squash-merge commits on master carry a signature today.
- Its design notes record that an "automation signing key would require a separately approved key/trust lifecycle". On 2026-11-21 Aaron approved that path (option A).
- Argus already resolves `op://` references through the 1Password service account configured in `[secrets.op]` (`internal/agent/secretregistry.go`). The service account cannot use the 1Password SSH agent, but it can read an SSH Key item's fields.

## Goals / Non-Goals

**Goals:**

- Every agent commit is signed and shows as Verified on GitHub, with no Touch ID prompt, including unattended overnight work.
- The private key lives in 1Password and in argus's daemon memory only: never on disk, and never inside an agent process.
- A signing failure fails the commit loudly with a named cause. It never falls back to unsigned or to the personal key.
- `inherit` and `unsigned` keep their current behavior byte for byte.

**Non-Goals:**

- Changing how Aaron's own commits are signed (they stay on `op-ssh-sign`).
- Push or GitHub authentication (iris's `GH_PUSH_TOKEN` already covers pushes).
- Signing tags (`tag.gpgsign`). Can be added later if wanted.
- TUI, web, or macOS Settings controls. The setting stays config.toml-only like the existing policy. Settings controls remain the named follow-up `agent-git-policy-settings-controls`.
- Creating the key or registering it on GitHub. That is a one-time manual step by Aaron, documented in the README.

## Decisions

### D1. Argus holds the key, and agents ask argus to sign (chosen by Aaron)

The daemon resolves the private key once and keeps it in memory. Agent sessions get only the public key. Git in an agent session calls the `argus` binary as its signing program, and that call forwards the data to be signed to the daemon, which returns the signature.

*Alternative considered:* load the key into a private `ssh-agent` per session (`ssh-add -`) and point `SSH_AUTH_SOCK` at it. This is simpler, but every agent could use or copy the key, and overriding `SSH_AUTH_SOCK` changes how sessions authenticate to GitHub over SSH. Rejected.

### D2. Configuration

```toml
[git]
agent_commit_signing = "agent-key"
agent_signing_key = "op://claude/Argus agent signing key"
```

- `agent_signing_key` names a 1Password SSH Key item. Argus reads `<ref>/public key` and `<ref>/private key?ssh-format=openssh`.
- `agent_signing_key` is required when the policy is `agent-key`, and validation rejects it when it is missing or not an `op://` reference.
- Validation names all three allowed values (`inherit`, `unsigned`, `agent-key`).

*Alternative considered:* two separate references for the public and private halves. Rejected: one item is easier to manage, and the daemon checks that the halves match (D6).

### D3. Session environment (applied after backend env mappings, same slot as `unsigned`)

`agentGitPolicy` appends command-scope Git config through `GIT_CONFIG_COUNT`/`KEY_n`/`VALUE_n`, and through `GIT_CONFIG_PARAMETERS` when one is inherited, exactly as `unsigned` does today:

- `commit.gpgsign=true`
- `gpg.format=ssh`
- `user.signingkey=key::<public key>`
- `gpg.ssh.program=<absolute path of the running argus executable>`

The launcher resolves only the **public** key (`<ref>/public key`) through the existing secret resolver. That runs in whichever process calls `BuildCmd` (the supervisor, by default). If the public key does not resolve, the launch still proceeds, but the session is given `gpg.ssh.program` pointing at the helper, which then fails each commit with the named cause. It does not silently drop to unsigned.

### D4. The `argus` signing helper (`ssh-keygen` call-compatible)

Git invokes `gpg.ssh.program` with `ssh-keygen` arguments, not through a shell, so the helper cannot be an `argus <subcommand>`. `cmd/argus/main.go` dispatches to the helper when `os.Args[1] == "-Y"`:

- `-Y sign -n git -f <pubkey file> <data file>`: reads the data file and POSTs it with the namespace to the daemon. It writes the returned armored signature to `<data file>.sig`, the same contract as `ssh-keygen`. The helper accepts git's `-U` (key in agent) and `-O` options and ignores them.
- `-Y verify`, `-Y find-principals`, `-Y check-novalidate`: `exec` the real `ssh-keygen` with the same arguments, so `git log --show-signature` and `git verify-commit` behave normally.
- Daemon discovery and auth reuse the `argus artifact` path: the REST port over the Unix socket and the master API token from `~/.argus`. The helper sends `ARGUS_TASK_ID` for attribution.
- Retry: connection failures are retried for up to 30 seconds, to ride out a daemon bounce. Any HTTP error is final.
- On failure the helper exits non-zero, and stderr names the cause and says: "report this to your coordinator as a blocker; do not retry unsigned".

### D5. Daemon signing endpoint

`POST /api/git/sign` with `{namespace, data (base64), task_id}` returns `{signature}` (armored SSHSIG).

- Implemented in Go with `golang.org/x/crypto/ssh` (already a dependency): it builds the SSHSIG blob (`"SSHSIG"`, namespace, reserved, `sha512`, H(data)), signs it with the in-memory `ssh.Signer`, and armors the result. No `ssh-agent` or `ssh-keygen` subprocess runs on the signing path.
- It refuses any namespace other than `git` (HTTP 400).
- It returns HTTP 503 with a named cause when the policy is not `agent-key`, or the key is unresolved or mismatched.
- It logs `[agent-git] sign task=<id> ok|err=<cause>` for every request, never data or key material.
- The key is resolved lazily on first request and memoized for the life of the process, with success-only memoization like the secrets resolver. A failed resolve is retried on the next request, so a later 1Password recovery needs no daemon restart.

*Alternative considered:* host signing in the session-supervisor, which is longer-lived. Rejected: the REST surface, tokens, and the artifact-CLI discovery path all live in the daemon, and the helper's 30-second retry covers daemon bounces.

### D6. Key integrity and `argus doctor`

- When resolving the private key, the daemon derives its public key and compares it with `<ref>/public key`. A mismatch is a named error.
- `argus doctor` gains an advisory "agent signing key" line: `OFF` (policy is not `agent-key`), `OK` (both halves resolve and match), or `BROKEN: <cause>`. It is advisory only and never changes doctor's exit code, like the other advisory checks.

## Risks / Trade-offs

- [Any process that can reach the daemon, including any agent, can request a `git`-namespace signature] → The same trust boundary as `argus artifact` and the master token today. The key itself never leaves the daemon, signatures are limited to the `git` namespace, and every signature is logged with its task. A stolen signature is worth far less than a stolen key.
- [Signing depends on the daemon] → Agents run only while argus runs, the helper retries for 30 seconds across a bounce, and a longer outage fails loudly.
- [The 1Password service account loses access to the item] → Commits fail with a named cause, and `argus doctor` reports `BROKEN`.
- [The sandbox blocks the helper from reaching the daemon] → `argus artifact` already uses this exact discovery path from sandboxed sessions. The tests cover the helper's discovery, and a live smoke test is part of the tasks.
- [The supervisor's spawn surface changes] → Re-record the `SupervisorSpawnSurface` digest per `gotchas/ci-gates.md`. Running sessions keep their old environment until restarted or recycled, same as the existing policy.

## Migration Plan

1. Aaron creates an SSH Key item in the `claude` vault (1Password → New Item → SSH Key → Generate, Ed25519), named for example "Argus agent signing key".
2. Aaron adds its public key to GitHub as a **Signing Key** (Settings → SSH and GPG keys → New SSH key → Key type: Signing Key).
3. After deploy, Aaron sets `agent_commit_signing = "agent-key"` and `agent_signing_key = "op://claude/Argus agent signing key"`, runs `argus doctor` to confirm `OK`, then restarts or recycles sessions.
4. Rollback: set the policy back to `unsigned` (or `inherit`) and restart or recycle sessions.

## Open Questions

None. The two product decisions (where the key lives: in argus; what happens on failure: fail loudly) were made by Aaron on 2026-11-21.

## Discovery findings

- `agentGitPolicy` (`internal/agent/git_policy.go`) already implements the command-scope `GIT_CONFIG_*` append, including the inherited `GIT_CONFIG_PARAMETERS` quirk. The new value reuses it.
- `resolveBackendEnvSource` / `ResolverFor(cfg.Secrets)` resolve `op://` through the service-account bootstrap with process-lifetime success memoization.
- `cmd/argus/artifact.go` shows the sandbox-safe daemon discovery (socket → REST port, master token) that the helper reuses.
- `golang.org/x/crypto` is already in `go.mod` (indirect), so the SSHSIG implementation needs no new module.

## Acceptance criteria

- It should sign every agent commit with the agent key when the policy is `agent-key`, producing a signature `ssh-keygen -Y verify` accepts for the configured public key.
- It should never place the private key in an agent session's environment, arguments, or files.
- It should fail the commit with a named cause when the key cannot be resolved, the halves mismatch, or the daemon is unreachable for 30 seconds.
- It should refuse to sign any namespace other than `git`.
- It should leave `inherit` and `unsigned` sessions exactly as they behave today.
- It should reject `agent-key` without a valid `op://` `agent_signing_key`, naming the allowed values.
- It should pass `-Y verify` and other non-sign operations through to the real `ssh-keygen`.
- It should report the agent key's state in `argus doctor` as `OFF`, `OK`, or `BROKEN: <cause>`.
- It should log every signing request with its task ID and outcome, never the data or the key.
