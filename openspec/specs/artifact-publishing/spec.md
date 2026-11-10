# artifact-publishing Specification

## Purpose

The Argus daemon publishes single-page claude.ai artifacts to a named account using that account's `claude setup-token` token. Agents reach it through the `artifact_publish` and `artifact_open` MCP tools; people reach it through `POST /api/artifacts/publish` and `argus artifact publish`. Tokens never reach agents. Every path an agent hands the unsandboxed daemon is confined to what that agent could already access. When the target account is unavailable, for example out of budget, the page is kept as a task artifact and the user decides what happens next.

## Requirements
### Requirement: Per-account artifact tokens

The system SHALL read an `[artifact_tokens]` config table mapping an account name to a secret source string. It SHALL resolve the source through the secrets resolver inside the daemon, at publish time. It SHALL reject a value that is not a secret reference. The resolved token SHALL NOT appear in any agent environment, MCP tool result, REST response, error message, or log line.

#### Scenario: Token resolved at publish time

- **WHEN** `artifact_publish` is called for account `personal` and `[artifact_tokens]` maps `personal` to an `op://` reference
- **THEN** the daemon resolves that reference and sends the token as a bearer credential to the artifacts endpoint

#### Scenario: No token configured

- **WHEN** `artifact_publish` is called for an account with no `[artifact_tokens]` entry
- **THEN** the call fails with an error naming the account and the `[artifact_tokens]` table, and no network request is made

#### Scenario: Token never leaks

- **WHEN** any publish succeeds, fails, or finds the account unavailable
- **THEN** the token string does not appear in the tool result, the REST response body, the error text, or the log output

### Requirement: Publish a single HTML page

The system SHALL publish a single HTML file to the claude.ai artifacts endpoint (`https://api.anthropic.com/api/artifacts/mcp`) as the given account, and return the artifact's claude.ai link. Given an existing artifact link, it SHALL instead publish a new version on top of that artifact's current version. It SHALL reject, before any network call, a file that does not have an `.html` or `.htm` extension, does not begin with an HTML doctype or `<html`, or exceeds 16 MiB. An HTTP 401 from the endpoint SHALL produce an error telling the user to re-run `claude setup-token` for that account.

#### Scenario: New artifact

- **WHEN** `artifact_publish` is called with an HTML file, a title, and a configured account
- **THEN** a new private artifact is created on that account and the result contains its claude.ai link

#### Scenario: Update existing artifact

- **WHEN** `artifact_publish` is called with an HTML file and the link of an existing artifact on that account
- **THEN** the system looks up the artifact's current version and publishes the file as a new version based on it, returning the same link

#### Scenario: Invalid input rejected locally

- **WHEN** `artifact_publish` is called with a non-HTML file or a file larger than 16 MiB
- **THEN** the call fails with a descriptive error and no network request is made

#### Scenario: Expired token

- **WHEN** the artifacts endpoint answers HTTP 401
- **THEN** the error tells the user to re-run `claude setup-token` for that account and update its secret

### Requirement: Unavailable account handling

The system SHALL treat an HTTP 404 response to a tool call whose body says the artifact tools "aren't available to this session" as the account being unavailable. When the account is unavailable, the system SHALL register the HTML file as an artifact of the calling task. It SHALL probe every other account in `[artifact_tokens]` with a real tool call, and SHALL return a non-error result containing `status: "unavailable"`, the requested account, the registered artifact filename, the saved copy's absolute path (`artifact_path`), the call's `update` link when there was one, and the accounts whose probe succeeded.

#### Scenario: Exhausted home account

- **WHEN** `artifact_publish` is called for account `default` from a task and the endpoint refuses with the "aren't available to this session" 404
- **THEN** the HTML file is registered as an artifact of that task, and the result has `status: "unavailable"`, account `default`, the artifact filename, its absolute `artifact_path`, and the list of other accounts that can publish

#### Scenario: Other account also unavailable

- **WHEN** the home account is unavailable and the probe of another configured account is also refused
- **THEN** that other account is not listed as available

### Requirement: Confine agent-supplied paths

The daemon runs outside the agent sandbox, so it SHALL NOT read a file on an agent's behalf from outside that agent's own task. `artifact_publish` SHALL require a resolvable calling task and SHALL fail, before reading any file or making any request, when none resolves. For every task, sandboxed or not, `artifact_publish` and `artifact_register` (single files, a registered folder itself, and every member of a folder) SHALL:

- make a relative path absolute against the request's `cwd`, or the task worktree when `cwd` is empty, never the daemon's working directory;
- open the path once without following a symlink in its final component, and refuse a symlink (including a symlinked folder);
- decide file versus folder from that open descriptor, never from a lookup by name before confinement;
- require, from the open descriptor, a regular file with exactly one hard link;
- take the file's location from the open descriptor (on macOS, the filesystem's canonical path and case) and refuse it unless it lies inside the task's worktree, the sandbox profile's writable temp directories or the task's configured `extra_write` paths, and outside the sandbox's built-in and configured deny-read paths, with those roots and paths canonicalized the same way;
- require the open descriptor and that location to be the same file, check the descriptor again (still a regular file with one link), and read only from that descriptor, enforcing the size cap while reading.

Every refusal SHALL return the same generic error ("path is not an allowed artifact source"), whatever the reason (missing, unreadable, not regular, symlink, hard link, outside the roots, deny-read), so the tools cannot reveal whether a file exists. The specific reason SHALL be logged daemon-side under `[artifacts]`, without file contents. Off macOS the location comes from resolving the name, which does not canonicalize case.

The calling task is self-reported (`id` or `cwd`): the MCP endpoint takes no per-task credential, so no connection is authenticated as a task. Naming another task SHALL therefore confine the read to that task's roots, never widen it further. The REST endpoint and CLI carry the page's content in the request body, never a server-side path.

#### Scenario: No calling task

- **WHEN** `artifact_publish` is called with neither a resolvable task `id` nor a `cwd` inside a task's worktree
- **THEN** the call fails, nothing is read or registered, and no network request is made

#### Scenario: Publishing from the task's own roots

- **WHEN** a task calls `artifact_publish` with a file inside its worktree or a writable temp directory
- **THEN** the file is published

#### Scenario: Path outside the roots refused

- **WHEN** a task, sandboxed or not, calls `artifact_publish` or `artifact_register` with a path that resolves outside its writable directories
- **THEN** the call fails with the generic not-an-allowed-source error, and the file is not read

#### Scenario: Symlink final component refused

- **WHEN** the given path is itself a symlink
- **THEN** the call fails and the symlink's target is not read

#### Scenario: Symlinked parent pointing outside refused

- **WHEN** a directory component of the given path is a symlink to a directory outside the task's writable directories
- **THEN** the call fails and the file is not read

#### Scenario: Hard-linked file refused

- **WHEN** the given file has more than one hard link
- **THEN** the call fails and the file is not read; a hard-linked member of a registered folder is skipped

#### Scenario: Deny-read path refused

- **WHEN** the given path lies inside a writable root but under a built-in or configured sandbox deny-read path
- **THEN** the call fails and the file is not read

#### Scenario: Case-variant spelling of a deny-read path refused

- **WHEN** on a case-insensitive filesystem the given path spells a deny-read directory in a different case (for example `SECRETS` for `secrets`)
- **THEN** the call fails and the file is not read

#### Scenario: Refusals reveal nothing about the file

- **WHEN** calls name an existing file, an existing folder, a hidden folder and nonexistent paths outside the task's writable directories
- **THEN** all fail with the identical generic error, and the specific reason is only logged by the daemon

#### Scenario: Relative path resolved against the agent's directory

- **WHEN** an agent passes a relative path with a `cwd`, or with no `cwd`
- **THEN** it resolves against that `cwd`, or the task worktree, and is confined from there; the daemon's own working directory is never used

#### Scenario: Spoofed task id confined to that task

- **WHEN** an agent passes another task's `id` and a path outside that task's writable directories
- **THEN** the call fails and the file is not read

### Requirement: Open or reveal a registered artifact

The system SHALL provide an `artifact_open` MCP tool that opens a registered artifact of the calling task in the default application, or reveals it in Finder when `reveal` is true. It SHALL refuse any filename that is not in the calling task's artifact manifest or that fails the artifact path-escape defense. Because the daemon's `open` runs outside the agent sandbox, opening in the default application SHALL be allowed only when the artifact's stored type is HTML, PDF, image, Markdown, text, audio or video AND its extension is one of that type's known extensions (for example `.html`/`.htm`, `.pdf`, `.png`/`.jpg`/`.jpeg`/`.gif`/`.webp`/`.svg`, `.md`, `.txt`, `.mp3`/`.m4a`/`.wav`, `.mp4`/`.mov`); anything else SHALL be refused with an error suggesting `reveal`. Revealing SHALL be allowed for any registered file, but a path that is a directory or an app bundle (such as `.app`) SHALL be refused even for reveal. On a platform other than macOS it SHALL return a not-supported error.

#### Scenario: Open in default app

- **WHEN** `artifact_open` is called with a filename registered to the calling task and `reveal` false
- **THEN** the daemon opens that file with the system's default application

#### Scenario: Reveal in Finder

- **WHEN** `artifact_open` is called with a registered filename and `reveal` true
- **THEN** the daemon reveals that file in Finder

#### Scenario: Executable artifact can only be revealed

- **WHEN** `artifact_open` is called for a registered `.command` or `.sh` artifact with `reveal` false
- **THEN** the call fails with an error suggesting `reveal: true`, and nothing is opened; with `reveal` true the file is revealed in Finder

#### Scenario: Type and extension mismatch refused

- **WHEN** `artifact_open` is called with `reveal` false for an artifact whose extension does not match its stored type (for example an `.html` file registered as `pdf`)
- **THEN** the call fails and nothing is opened

#### Scenario: Directory or bundle refused

- **WHEN** `artifact_open` is called for a registered path that is a directory or an app bundle, with or without `reveal`
- **THEN** the call fails and nothing is opened or revealed

#### Scenario: Unregistered file refused

- **WHEN** `artifact_open` is called with a filename that is not registered to the calling task
- **THEN** the call fails and nothing is opened

### Requirement: Publish from the command line and REST

The system SHALL expose `POST /api/artifacts/publish` (authenticated like the other REST endpoints) and a CLI subcommand `argus artifact publish <file> --account <name> [--title <t>] [--update <link>]` that publishes through it. On success the CLI SHALL print the artifact link and exit 0. When the account is unavailable it SHALL exit with code 3 and print the accounts that can publish. On any other failure it SHALL exit non-zero with the error.

#### Scenario: CLI success

- **WHEN** a user runs `argus artifact publish page.html --account personal` and the publish succeeds
- **THEN** the artifact link is printed and the exit code is 0

#### Scenario: CLI unavailable account

- **WHEN** a user runs `argus artifact publish page.html --account default` and the `default` account is unavailable
- **THEN** the command exits with code 3 and prints which configured accounts can publish

