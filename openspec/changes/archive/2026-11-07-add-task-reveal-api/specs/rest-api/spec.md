## ADDED Requirements

### Requirement: Reveal a contained task path on the daemon host

`POST /api/tasks/{id}/reveal` with JSON `{"path": "<relative>"}` or `{}` SHALL require the master token and open the task's worktree directory (`open -a Finder <dir>`, `"target":"worktree"`) when no path is given, or reveal one regular file (`open -R <file>`, `"target":"file"`) otherwise, responding 200 `{"status":"opened","target":...,"host":"daemon"}` only after the launcher returns success. The command SHALL run with an argument vector, never a shell, and clients cannot choose the application or command. The body SHALL contain one JSON value followed only by whitespace (an empty body is also allowed). Worktree mode SHALL require a real directory at the stored path, checked with Lstat; a final symlink or non-directory SHALL return 400 `invalid_path` without launching. Finder SHALL be selected explicitly so a post-check replacement cannot activate another application. Immediately before launch the server SHALL resolve symlinks and require the resolved path to stay inside the resolved worktree; absolute paths, `..` segments, NUL bytes, any `.git` segment (before or after resolution, case-insensitive), symlink escapes, and non-regular files SHALL return 400 `invalid_path` without launching. An unknown task or a task with no worktree SHALL return 404 `task_not_found`; a missing worktree or path 404 `path_not_found`; a non-darwin host 501 `unsupported_host`; a launcher failure 500 `reveal_failed`. No SSE event is emitted.

#### Scenario: File with spaces
- **WHEN** a master client reveals `sub/my file.txt`
- **THEN** the launcher receives `open`, `-R`, and the resolved path as exactly one argument

#### Scenario: Symlink escape
- **WHEN** the path resolves outside the worktree
- **THEN** the response is 400 `invalid_path` and the launcher is not invoked

#### Scenario: Device token
- **WHEN** a device token calls the route
- **THEN** the response is 403 and nothing launches
