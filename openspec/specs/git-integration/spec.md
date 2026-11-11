# Git Integration

## Purpose

Surface the git state of each task's worktree inside the TUI: which files changed, a diff summary, the per-branch diff against the merge-base, and per-file unified diffs rendered side-by-side. Git inspection runs entirely off the UI thread via background goroutines that deliver results as messages, so the interface never blocks on a slow `git` invocation. All operations are read-only inspection of a worktree; they never mutate repository state.
## Requirements
### Requirement: Worktree status, summary, and branch-diff capture

The system SHALL collect a worktree's short status, an optional working-tree diff summary, and a per-branch diff against the merge-base in a single fetch, returning a result tagged with the originating task ID. An empty worktree path SHALL return an empty result except for the task ID. Failure to collect status or a resolved branch comparison SHALL set the result's error; absent optional summary or unavailable history, including an unborn HEAD, SHALL leave those fields empty.

#### Scenario: No worktree path

- **WHEN** a status fetch is requested with an empty worktree path and task ID "task-1"
- **THEN** the result carries task ID "task-1" with empty status, diff, and branch-diff fields

#### Scenario: Clean repository

- **WHEN** a status fetch runs against a worktree with no changes
- **THEN** the status field is empty and the result is tagged with the requesting task ID

#### Scenario: Dirty worktree

- **WHEN** a tracked file is modified and an untracked file is added in the worktree
- **THEN** the status field references both files and the diff summary references the modified file

#### Scenario: Branch with an extra commit

- **WHEN** the worktree is on a branch that has one commit beyond the merge-base
- **THEN** the branch-diff summary and the branch file list both reference the file changed by that commit

### Requirement: Merge-base resolution with upstream and default-branch fallback

The system SHALL determine the comparison base for branch diffs by first trying the task's requested base as a local ref and then as `origin/<base>`, before trying the branch's configured upstream and falling back to remote-tracking default branches (`upstream/master`, `origin/master`, `upstream/main`, `origin/main`, in that order), then finally to local `master`/`main` branches. Both local and remote-tracking branch refs are shared across every worktree of a repository (only `HEAD` is per-worktree), but preferring remote-tracking refs still avoids resolving the merge-base against a local branch ref that another process (e.g. a different worktree or the primary checkout) could arbitrarily rewrite via its own rebase, reset, or amend — remote-tracking refs only change via an explicit fetch, a deliberate sync to real upstream state rather than an arbitrary history rewrite. When none of these resolve (for example, the path is not a git repository, or it has neither a remote nor a local `master`/`main`), the system SHALL yield an empty base and skip branch-diff collection.

#### Scenario: No merge base available

- **WHEN** merge-base resolution runs against a path that is not a git repository
- **THEN** the resolved base is empty

#### Scenario: Falls back to a remote-tracking default branch

- **WHEN** a feature branch has no upstream configured but an `origin/master` remote-tracking branch exists
- **THEN** the resolved merge-base is computed against `origin/master` rather than any local branch

#### Scenario: Falls back to local master when no remote exists

- **WHEN** a feature branch has no upstream and no remote-tracking branches, but a local `master` branch exists
- **THEN** a non-empty merge-base is resolved against local `master`

#### Scenario: Falls back to main when master is absent

- **WHEN** a feature branch has no upstream, no remote-tracking branches, and only a local `main` branch exists
- **THEN** a non-empty merge-base is resolved via the `main` fallback

### Requirement: Per-file diff with uncommitted, branch, and untracked fallbacks

The system SHALL produce a unified diff for a single file, preferring staged and unstaged changes against HEAD, or staged changes against the empty tree when HEAD is unborn, then committed branch changes against the resolved merge-base. Only an untracked file with no earlier patch MAY use the full-content added diff fallback; a tracked unchanged file SHALL remain empty. The untracked fallback SHALL use literal paths and refuse paths outside the worktree. It SHALL read regular-file content through a worktree-root handle that enforces containment during open, then render a private snapshot rather than letting Git reopen a mutable worktree path. The snapshot SHALL preserve the displayed relative path, file mode and symlink text, and render captured symlinks as private Git objects rather than filesystem links, and SHALL cap captured content, including applicable working-tree attributes, at 16 MiB and return a collection error on overflow rather than truncating the patch. A non-repository or missing worktree directory SHALL set the result's error. An empty worktree path or empty file path SHALL yield an empty diff.

#### Scenario: Empty inputs

- **WHEN** a file diff is requested with an empty worktree path, or with an empty file path
- **THEN** the returned diff is empty

#### Scenario: Uncommitted modification

- **WHEN** a tracked file has uncommitted edits
- **THEN** the returned diff references the file and includes the changed content

#### Scenario: Untracked file

- **WHEN** a file diff is requested for an untracked file
- **THEN** the returned diff references the file's contents as an addition

#### Scenario: Branch-only commit

- **WHEN** a file was changed only by a commit on the current branch with no uncommitted edits
- **THEN** the returned diff against the merge-base references the file

#### Scenario: Staged first file in an unborn repository

- **WHEN** a repository has no HEAD and its first file is staged
- **THEN** the returned patch includes the staged file's added content

#### Scenario: Tracked unchanged file has no synthetic addition

- **WHEN** a tracked file is unchanged against both the working tree and comparison base
- **THEN** its returned diff is empty

#### Scenario: Invalid repository reports a collection error

- **WHEN** a file diff is requested from a non-repository or a missing worktree directory
- **THEN** the result carries an error and no synthetic patch

#### Scenario: Untracked symlink escapes the worktree

- **WHEN** an untracked path traverses a directory symlink to a file outside the resolved worktree root
- **THEN** the no-index fallback returns an empty diff without reading the target

#### Scenario: No-index operands remain literal

- **WHEN** an untracked filename starts with a git option or contains spaces
- **THEN** it is read only as a contained filename and cannot create or truncate an output destination

#### Scenario: Symlink changes after validation

- **WHEN** an in-tree directory symlink is replaced by an outside-pointing link between validation and Git rendering
- **THEN** the patch contains only bytes obtained from a contained opened handle, or is empty, and never contains outside bytes

#### Scenario: Final symlink target changes type

- **WHEN** a final in-tree symlink target becomes an outside-pointing directory after capture
- **THEN** the patch renders only the captured link text with mode `120000`, without opening the target or any child named `null`

#### Scenario: Bounded untracked snapshot

- **WHEN** an untracked regular file is larger than 16 MiB or grows beyond that bound during capture
- **THEN** the result carries a collection error and no truncated patch

#### Scenario: Text conversion cache remains read only

- **WHEN** a configured diff driver enables text conversion caching for an untracked diff
- **THEN** the patch matches native Git without changing repository refs or leaving invalid objects

#### Scenario: Captured regular files retain input conversion

- **WHEN** regular-file attributes enable clean filters or text and end-of-line conversion
- **THEN** the captured bytes undergo native path-aware input conversion before rendering, while symlink text remains raw

#### Scenario: Cache override keys preserve driver names

- **WHEN** a textconv driver name contains equals signs or dots and inherited indexed Git configuration is present
- **THEN** cache overrides preserve the complete key and inherited entries, with unchanged refs and a valid repository

#### Scenario: Textconv receives captured working bytes

- **WHEN** textconv is combined with clean, smudge or end-of-line conversion
- **THEN** it receives the raw captured file without a clean/smudge round trip, while patch hashes and ordinary patches retain native input conversion

#### Scenario: Relative untracked diff configuration

- **WHEN** an untracked file uses a repository-relative text converter or attributes file
- **THEN** the captured patch matches native Git and converter failure returns an error without a partial patch

#### Scenario: Canonical private index paths

- **WHEN** an untracked path includes `./` or literal pathspec syntax
- **THEN** the private index uses a canonical literal key while the patch preserves the requested operand spelling

#### Scenario: Final entry changes during capture

- **WHEN** a final file, directory or symlink changes after classification and before reading
- **THEN** capture either reads within the root or reports the entry unavailable without copying outside bytes

### Requirement: Directory file listing with path-traversal protection

The system SHALL list the untracked and modified files within a directory of the worktree, respecting gitignore rules, and SHALL annotate each entry with its actual git status code. The resolved directory path MUST remain inside the worktree; any path that escapes the worktree (for example via `..`) SHALL be rejected with an empty result, as SHALL an empty worktree path or empty directory path.

#### Scenario: Path traversal rejected

- **WHEN** a directory listing is requested for a path that resolves outside the worktree (e.g. "../escape")
- **THEN** the result contains no files

#### Scenario: Empty inputs

- **WHEN** a directory listing is requested with an empty worktree path, or with an empty directory path
- **THEN** the result contains no files

#### Scenario: Lists changed and untracked files

- **WHEN** a subdirectory contains a modified tracked file and a new untracked file
- **THEN** the result includes both files, each carrying its git status

### Requirement: Parsing git status and name-status output

The system SHALL parse `git status --short` and `git diff --name-status` output into changed-file entries carrying a status code, a path, and a directory flag (set when the path ends in a separator). Empty input SHALL produce no entries; malformed lines (status output shorter than the minimum length, or name-status lines lacking a tab separator) SHALL be skipped rather than producing garbage entries.

#### Scenario: Parse short status

- **WHEN** parsing short-status output containing a modified file, an untracked file, and an added file
- **THEN** each produces an entry with the correct status code and path

#### Scenario: Empty status input

- **WHEN** parsing an empty status string
- **THEN** no entries are produced

#### Scenario: Malformed lines skipped

- **WHEN** parsing name-status output that contains a line with no tab separator alongside a valid entry
- **THEN** only the valid entry is produced

### Requirement: Merging committed and uncommitted changed-file lists

The system SHALL merge a base changed-file list with an overlay list into a single deduplicated list sorted by path, where the overlay entry wins when the same path appears in both. This lets the file view present the full picture of branch changes (committed) plus working-tree changes (uncommitted) without duplicates. Merging two empty lists SHALL yield nothing.

#### Scenario: Overlay wins on conflict

- **WHEN** the same path appears in both the base and overlay lists with different statuses
- **THEN** the merged list contains one entry for that path carrying the overlay's status

#### Scenario: Both lists empty

- **WHEN** both the base and overlay lists are empty
- **THEN** the merged result is empty

### Requirement: Unified diff parsing into structured hunks

The system SHALL parse raw unified-diff text into a structured form exposing the old and new file names (with `a/` and `b/` prefixes stripped) and a sequence of hunks, each carrying its header, old/new line ranges, and classified content lines (context, added, removed) with correct old/new line numbers. Hunk headers with missing or zero ranges SHALL default to a start of 1 and a count of 1; the "No newline at end of file" marker SHALL NOT be emitted as a content line; and tabs in content SHALL be expanded to spaces.

#### Scenario: Parse a hunk

- **WHEN** parsing a unified diff for "file.go" with one hunk containing context, removed, and added lines
- **THEN** the parsed result reports both file names as "file.go" and a single hunk whose classified lines are recovered

#### Scenario: Hunk-range defaults

- **WHEN** parsing a hunk header that is malformed or specifies a zero start
- **THEN** the hunk's old and new starts default to 1

#### Scenario: No-newline marker excluded

- **WHEN** parsing a diff that contains a "No newline at end of file" marker line
- **THEN** that marker is not present as a content line in the hunk

### Requirement: Side-by-side diff layout

The system SHALL convert parsed diff hunks into side-by-side rows for display, emitting each hunk's header as a row, a separator between consecutive hunks, context lines on both sides, and pairing each run of removed lines against the following run of added lines so changes align left-to-right. The system SHALL also format line numbers to a fixed width (blank when zero, right-truncated to the last digits when too wide).

#### Scenario: Build side-by-side rows

- **WHEN** converting a parsed diff with a removed line, an added line, and a context line into side-by-side rows
- **THEN** the first row is the hunk header and subsequent rows pair the change and carry the context

#### Scenario: Line-number formatting

- **WHEN** formatting line number 0 to width 4
- **THEN** the result is four blank spaces

#### Scenario: Wide line-number truncation

- **WHEN** formatting line number 123456 to width 3
- **THEN** the result is the last three digits ("456")

### Requirement: Intra-line word diff

The system SHALL compute the changed character ranges between an old and a new line by tokenizing into word and non-word chunks, matching them via longest-common-subsequence, and mapping the unmatched tokens back to rune-offset spans (merging consecutive unmatched tokens into a single span). Identical lines SHALL produce no spans.

#### Scenario: Identical lines

- **WHEN** computing the word diff of two identical lines
- **THEN** no change spans are produced for either side

#### Scenario: Single word change

- **WHEN** the old line is "hello world" and the new line is "hello earth"
- **THEN** the changed span on each side covers only the differing word

#### Scenario: Consecutive unmatched tokens merged

- **WHEN** several adjacent tokens differ between the old and new lines
- **THEN** they are reported as a single merged change span

### Requirement: Remote branch enumeration with priority ordering

The system SHALL enumerate remote-tracking branch names for a repository, excluding any `HEAD` pseudo-ref, and SHALL order them with the preferred branches (`upstream/master`, `origin/master`, `upstream/main`, `origin/main`, in that order) first, followed by the remaining branches sorted alphabetically. An empty path, or a path that is not a git repository, SHALL yield nothing.

#### Scenario: Priority ordering

- **WHEN** enumerating branches that include `origin/master` and `origin/HEAD` plus other branches
- **THEN** `origin/master` appears first and `origin/HEAD` is excluded

#### Scenario: No git repository

- **WHEN** enumerating branches for an empty path or a path that is not a git repository
- **THEN** the result is empty

### Requirement: Git status panel rendering

The git status panel SHALL render a bordered "Git Status" view showing a "Loading..." placeholder before any data is set; once loaded it SHALL show non-empty Files, Diff, and Branch sections, render an empty-state message when all sections are empty, and color status lines by their status code (modified, added/untracked, deleted). Long lines SHALL be truncated with an ellipsis to fit the panel width.

#### Scenario: Initial and loaded state

- **WHEN** the panel is created and then given status, diff, and branch content
- **THEN** it transitions from not-loaded to loaded and retains the parsed section line counts

#### Scenario: Clearing the panel

- **WHEN** the panel is cleared after holding content
- **THEN** it returns to the not-loaded state with no section lines

#### Scenario: Long line truncation

- **WHEN** a line wider than the available width is rendered
- **THEN** it is truncated and the final visible character is an ellipsis


### Requirement: Task base comparison

Status, branch-file, and per-file diff collection SHALL resolve the task's stored base branch (local ref, then `origin/<base>`) as the merge-base source before the upstream and default-branch fallbacks, and SHALL report a comparison record with `requested_base`, `resolved_base`, `merge_base`, and `fallback_reason`. A requested base that is missing, option-like, or shares no history SHALL fall back to the existing chain and set a non-empty `fallback_reason`.

#### Scenario: Stacked child excludes parent commits
- **WHEN** a child branch stacked on `parent` is inspected with base `parent` while upstream tracks an unrelated ref
- **THEN** branch files list only the child's changes and the merge-base is the parent tip

#### Scenario: Missing base
- **WHEN** the requested base does not exist
- **THEN** the fallback base is used and `fallback_reason` is non-empty

#### Scenario: Tracked unchanged file
- **WHEN** a per-file diff is requested for a tracked file with no change against the base or working tree
- **THEN** the diff is empty

## Related API contracts

See `rest-api` for the task reveal contract, including master authorization, contained paths, Finder launch and error responses.
