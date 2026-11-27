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

### Requirement: Child-only Git configuration isolation

The signing override MUST be injected only into the spawned agent process environment, preserving existing command-scope Git entries and applying after backend environment mappings. Argus MUST NOT modify global Git configuration, SSH configuration, keychain data, or shared repository configuration to apply the policy. Explicit Git command-line options SHALL retain Git's normal precedence. The policy SHALL be documented as process-scoped rather than branch-enforced.

#### Scenario: Existing command-scope configuration

- **WHEN** an `unsigned` or `agent-key` session inherits command-scope Git settings
- **THEN** unrelated entries remain effective and the policy's entries are appended after them, so the configured signing policy deterministically governs default commit signing

#### Scenario: Host isolation

- **WHEN** an `unsigned` or `agent-key` agent session is launched
- **THEN** a Git command launched outside that agent process tree continues to observe its prior signing configuration and all host configuration files remain unchanged

#### Scenario: Explicit signing requested

- **WHEN** a caller passes an explicit signing option to Git within an `unsigned` or `agent-key` session
- **THEN** Git processes that option according to its normal precedence (an explicit `--no-gpg-sign` can still produce an unsigned commit; Argus itself never falls back to unsigned)


## ADDED Requirements

### Requirement: Daemon-held agent signing key

When the policy is `agent-key`, the daemon SHALL resolve the private key from `<agent_signing_key>/private key?ssh-format=openssh` (the default PKCS#8 value does not parse) through the configured `[secrets.op]` service account and hold it in memory only. The private key MUST NOT be written to disk and MUST NOT be placed in any agent session's environment, arguments, or files. Agent sessions SHALL receive only the public key. The daemon's key holder SHALL memoize resolution on success only, so a failed resolution is retried on the next signing request. Neither the daemon's key fields nor the launcher's public-key field SHALL be cached by the process-wide secret memo, so a key change or a changed reference is picked up without restarting the session-supervisor, and an old key is not retained. The daemon MUST verify that the public key derived from the private key matches `<agent_signing_key>/public key`.

#### Scenario: Rotated key picked up by new sessions

- **WHEN** the key behind the same reference is rotated in 1Password
- **THEN** sessions launched afterwards receive the new public key without a supervisor restart, and the helper's mismatch error for older sessions or a not-yet-restarted daemon names the remedy (restart the daemon, then restart or recycle the session)

#### Scenario: Public key unresolved at launch

- **WHEN** a session launched while its public key could not be resolved attempts a commit
- **THEN** the commit fails with a cause naming the unresolved agent signing public key and the remedy, without contacting the daemon

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

The daemon SHALL expose `POST /api/git/sign`, which accepts a namespace, data, and task ID and returns an armored SSHSIG signature produced in process with the agent key. The service MUST refuse any namespace other than `git`. It MUST answer with a named error when the policy is not `agent-key` or the key is unavailable. It SHALL log each request with task ID and outcome, and MUST NOT log the data or key material. The route MUST accept only loopback callers presenting the daemon's master token. Per-device tokens and non-loopback callers, such as Tailscale peers, MUST be refused.

#### Scenario: Remote device token refused

- **WHEN** a request arrives from a non-loopback address, or carries a per-device token instead of the master token
- **THEN** the service refuses it and produces no signature

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

When invoked with `-Y` as its first argument, the argus executable SHALL act as Git's `gpg.ssh.program`. For `-Y sign`, it SHALL read the data file and request a signature from the daemon. It MUST verify that the returned signature is a valid `git`-namespace SSHSIG over that data made by the public key Git passed with `-f`, and it SHALL write it to `<data file>.sig` only when that check passes, exiting zero. For every other `-Y` operation, it SHALL execute the system `ssh-keygen` with the same arguments. Every daemon call the helper makes, including discovery, MUST be bounded by a timeout.

#### Scenario: Signature from a different key rejected

- **WHEN** the daemon returns a signature made by a key other than the session's configured public key (for example, after the key was rotated and the daemon has not reloaded)
- **THEN** the helper fails the commit with a named key-mismatch cause and writes no `.sig`

#### Scenario: Wedged daemon

- **WHEN** the daemon accepts the connection but never answers discovery or signing
- **THEN** the helper times out and fails the commit with a named cause instead of hanging

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

`argus doctor` SHALL report the agent signing key as `OFF` when the policy is not `agent-key`, `OK` when both key halves resolve and match and `api.enabled` is true, or `BROKEN: <cause>` otherwise. A disabled REST API SHALL be reported as `BROKEN: api.enabled is false`, because the helper reaches the daemon only over REST. The missing prerequisite MUST NOT be made a configuration validation error, because a validation failure discards the whole config.toml overlay. The check SHALL be advisory and MUST NOT change doctor's exit code, and it MUST NOT print key material.

#### Scenario: Healthy key

- **WHEN** the policy is `agent-key` and both halves resolve and match
- **THEN** doctor reports `OK` and its exit code is unaffected

#### Scenario: config.toml rejected

- **WHEN** config.toml selects `agent-key` but fails validation, so the overlay is discarded
- **THEN** doctor reports `BROKEN: config.toml rejected: <validation error>` instead of `OFF`

#### Scenario: REST API disabled

- **WHEN** the policy is `agent-key` and `api.enabled` is false
- **THEN** doctor reports `BROKEN: api.enabled is false`, and an agent commit fails immediately with that cause instead of retrying for 30 seconds

#### Scenario: Broken key

- **WHEN** the policy is `agent-key` and the key cannot be resolved
- **THEN** doctor reports `BROKEN` with the cause and its exit code is unaffected
