## ADDED Requirements

### Requirement: Discard one task file

`POST /api/tasks/{id}/git/discard` SHALL accept `{path, scope, delete_untracked}` (scope `local` only) and return 200 `{path, discarded:true, scope}`. Invalid paths return 400 `invalid_path`; unknown task or worktree 404; untracked without the flag, renames, conflicts and clean paths 409; git failures 500, each as `{"error","code"}`. No event is emitted on failure.

#### Scenario: Untracked requires flag
- **WHEN** an untracked file is discarded without `delete_untracked`
- **THEN** the response is 409 `untracked_requires_delete` and the file remains
