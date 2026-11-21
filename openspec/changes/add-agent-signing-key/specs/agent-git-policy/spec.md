## MODIFIED Requirements

### Requirement: Explicit agent commit-signing policy

Argus SHALL support `git.agent_commit_signing` with values `inherit`, `unsigned`, and `agent-key`. The default SHALL be `inherit`; unsupported values MUST fail configuration validation with an actionable message that names all three allowed values. When the value is `agent-key`, `git.agent_signing_key` MUST be an `op://` reference, and validation MUST reject a missing or non-`op://` reference. The policy SHALL apply through the common launcher to agent sessions regardless of initiating frontend or backend.

#### Scenario: Existing installations preserve signing

- **WHEN** the setting is absent or is `inherit`
- **THEN** Argus introduces no signing override and Git inherits the existing configuration

#### Scenario: Unattended signing policy enabled

- **WHEN** the setting is `unsigned` and Argus launches an agent session
- **THEN** ordinary commits in that process tree default to `commit.gpgsign=false` without invoking a configured signer

#### Scenario: Agent key policy enabled

- **WHEN** the setting is `agent-key` with a valid `agent_signing_key` and Argus launches an agent session
- **THEN** the session's command-scope Git configuration sets `commit.gpgsign=true`, `gpg.format=ssh`, `user.signingkey=key::<agent public key>`, and `gpg.ssh.program` to the absolute path of the argus executable

#### Scenario: Invalid policy

- **WHEN** configuration contains a value other than `inherit`, `unsigned`, or `agent-key`
- **THEN** validation rejects it and names the allowed values

#### Scenario: Agent key policy without a key reference

- **WHEN** the setting is `agent-key` and `agent_signing_key` is empty or not an `op://` reference
- **THEN** validation rejects the configuration and names `git.agent_signing_key`

## ADDED Requirements

### Requirement: Daemon-held agent signing key

When the policy is `agent-key`, the daemon SHALL resolve the private key from `<agent_signing_key>/private key` through the configured `[secrets.op]` service account and hold it in memory only. The private key MUST NOT be written to disk and MUST NOT be placed in any agent session's environment, arguments, or files. Agent sessions SHALL receive only the public key. Resolution SHALL be memoized on success only, so a failed resolution is retried on the next signing request. The daemon MUST verify that the public key derived from the private key matches `<agent_signing_key>/public key`.

#### Scenario: Private key never reaches a session

- **WHEN** an agent session is launched under the `agent-key` policy
- **THEN** the session environment, arguments, and launch files contain the public key and no private key material

#### Scenario: Recovery after a failed resolution

- **WHEN** the private key fails to resolve on one signing request and resolves on a later request
- **THEN** the later request signs successfully without a daemon restart

#### Scenario: Mismatched key halves

- **WHEN** the private key's derived public key differs from the item's public key field
- **THEN** signing fails with a named mismatch cause and no signature is produced

### Requirement: Git-only signing service

The daemon SHALL expose `POST /api/git/sign`, authenticated like other daemon REST routes, which accepts a namespace, data, and task ID and returns an armored SSHSIG signature produced in process with the agent key. The service MUST refuse any namespace other than `git`. It MUST answer with a named error when the policy is not `agent-key` or the key is unavailable. It SHALL log each request with task ID and outcome, and MUST NOT log the data or key material.

#### Scenario: Git signature produced

- **WHEN** a valid request with namespace `git` arrives under the `agent-key` policy
- **THEN** the response contains a signature that `ssh-keygen -Y verify -n git` accepts for the agent public key

#### Scenario: Other namespace refused

- **WHEN** a request names a namespace other than `git`
- **THEN** the service refuses it and produces no signature

#### Scenario: Request logged without secrets

- **WHEN** any signing request completes or fails
- **THEN** a log line records the task ID and outcome, and contains neither the signed data nor key material

### Requirement: ssh-keygen-compatible signing helper

When invoked with `-Y` as its first argument, the argus executable SHALL act as Git's `gpg.ssh.program`. For `-Y sign`, it SHALL read the data file, request a signature from the daemon, and write it to `<data file>.sig`, exiting zero on success. For every other `-Y` operation, it SHALL execute the system `ssh-keygen` with the same arguments.

#### Scenario: Commit signed through the helper

- **WHEN** an agent session under the `agent-key` policy runs `git commit`
- **THEN** the commit carries an SSH signature that verifies against the agent public key, and no interactive prompt occurs

#### Scenario: Verification passes through

- **WHEN** Git invokes the helper with `-Y verify` or `-Y find-principals`
- **THEN** the system `ssh-keygen` runs with the same arguments and its result is returned unchanged

### Requirement: Fail-loud signing

A signing failure MUST fail the commit. The helper SHALL retry connection failures to the daemon for up to 30 seconds and MUST treat any daemon error response as final. On failure it MUST exit non-zero with a message that names the cause and instructs the agent to report it to its coordinator as a blocker. Argus MUST NOT fall back to an unsigned commit or to the personal signing key.

#### Scenario: Key unavailable

- **WHEN** the agent key cannot be resolved and an agent commits
- **THEN** the commit fails, the error names the unresolved key, and no unsigned commit is created

#### Scenario: Daemon briefly unavailable

- **WHEN** the daemon is unreachable for less than 30 seconds during a commit
- **THEN** the helper retries and the commit succeeds signed once the daemon answers

#### Scenario: Daemon unavailable too long

- **WHEN** the daemon is unreachable for 30 seconds during a commit
- **THEN** the commit fails with a named cause and no unsigned commit is created

### Requirement: Agent signing key doctor check

`argus doctor` SHALL report the agent signing key as `OFF` when the policy is not `agent-key`, `OK` when both key halves resolve and match, or `BROKEN: <cause>` otherwise. The check SHALL be advisory and MUST NOT change doctor's exit code, and it MUST NOT print key material.

#### Scenario: Healthy key

- **WHEN** the policy is `agent-key` and both halves resolve and match
- **THEN** doctor reports `OK` and its exit code is unaffected

#### Scenario: Broken key

- **WHEN** the policy is `agent-key` and the key cannot be resolved
- **THEN** doctor reports `BROKEN` with the cause and its exit code is unaffected
