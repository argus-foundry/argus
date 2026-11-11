## ADDED Requirements

### Requirement: Pi dash-prefixed prompts remain messages
When delivering a positional prompt to the Pi family, Argus SHALL prefix a newline if the prompt begins with `-`, so Pi receives it as a message rather than a CLI option. All original prompt content SHALL remain intact and shell-quoted. Normalization SHALL be logged without logging prompt content. Non-dash-prefixed prompts, configured prompt-flag delivery, resume behavior, and other backend families SHALL remain unchanged.

#### Scenario: Markdown frontmatter
- **WHEN** a new Pi task has a prompt beginning with `---title:`
- **THEN** the positional prompt starts with a newline followed by the complete original prompt
- **AND** Pi treats it as a message rather than an unknown option

#### Scenario: Option-like prose
- **WHEN** a new Pi task has a prompt beginning with `--title` or `-x`
- **THEN** the positional prompt starts with a newline followed by the complete original prompt

#### Scenario: Ordinary prompt
- **WHEN** a new Pi task's prompt does not begin with a dash
- **THEN** its prompt delivery is unchanged

## MODIFIED Requirements

### Requirement: Command construction with prompt and worktree isolation

The system SHALL build the agent command as a shell invocation whose working directory is the task's worktree. The prompt, when present, SHALL be passed using the backend's configured prompt flag, or — when no prompt flag is configured — as a positional argument guarded by a `--` end-of-options separator so prompts beginning with `-` are not parsed as flags. Pi positional prompts SHALL instead omit the separator and use the newline protection defined by the Pi dash-prefixed prompt requirement, because Pi does not honor `--`. The system SHALL refuse to build a command for a task with no worktree, and SHALL refuse when the worktree directory does not exist or is unreachable, returning an actionable error and a nil command.

#### Scenario: Prompt passed via configured prompt flag
- **WHEN** the backend defines a prompt flag and the task has a prompt
- **THEN** the command appends the prompt flag followed by the safely quoted prompt

#### Scenario: Prompt passed positionally with separator when no flag
- **WHEN** the backend defines no prompt flag, is not Pi, and the task has a prompt
- **THEN** the command appends `--` followed by the safely quoted prompt

#### Scenario: Missing worktree is rejected
- **WHEN** a task has no worktree set
- **THEN** command construction fails with an error stating no worktree is set

#### Scenario: Nonexistent worktree directory is rejected pre-launch
- **WHEN** the task's worktree path does not exist
- **THEN** command construction fails with an error naming the missing path, and returns a nil command and nil cleanup

#### Scenario: Working directory and task environment are set
- **WHEN** a command is built for a task with an ID
- **THEN** the command's working directory is the worktree and the environment exports the task ID for sub-agent tooling
