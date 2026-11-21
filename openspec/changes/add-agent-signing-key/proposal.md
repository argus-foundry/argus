## Why

Aaron wants every commit signed without repeated Touch ID prompts. Today agent sessions can either sign with his personal 1Password key (`inherit`), which prompts every session and stalls unattended work, or skip signing (`unsigned`). Neither meets both goals.

## What Changes

- Add a third `git.agent_commit_signing` value, `agent-key`, plus a required `git.agent_signing_key` (`op://` reference to a 1Password SSH Key item).
- When `agent-key` is selected, agent sessions are configured (child-process environment only) to sign every commit through the `argus` binary acting as the `ssh-keygen` signing program.
- The daemon resolves the private key through the existing `[secrets.op]` service account, holds it in memory only, and serves `POST /api/git/sign`, which produces SSHSIG signatures limited to the `git` namespace.
- A signing failure fails the commit with a named cause, with no fallback to unsigned or to the personal key.
- `argus doctor` gains an advisory agent-signing-key line (`OFF` / `OK` / `BROKEN: <cause>`).
- README Reference and gotchas document the setup (create the 1Password item, register it on GitHub as a Signing Key).

## Capabilities

### New Capabilities

None.

### Modified Capabilities

- `agent-git-policy`: adds the `agent-key` policy, the in-daemon signing service, the signing helper, fail-loud behavior, and the doctor check. The existing `inherit` and `unsigned` behavior is unchanged.

## Impact

- `internal/config/git.go` (new value and field, validation)
- `internal/agent/git_policy.go`, `internal/agent/agent.go` (env injection, public-key resolution, log line)
- New daemon signing service and `POST /api/git/sign` route in `internal/api`
- `cmd/argus/main.go` dispatch for `-Y` and a new helper file in `cmd/argus`
- `cmd/argus/doctor.go` and `internal/doctor` (advisory check)
- Supervisor spawn-surface digest re-record; README Reference config table; `context/knowledge/gotchas/misc.md`
- No new module dependency (`golang.org/x/crypto/ssh` is already present). No TUI, web, or macOS UI change; Settings controls remain the named follow-up `agent-git-policy-settings-controls`.
