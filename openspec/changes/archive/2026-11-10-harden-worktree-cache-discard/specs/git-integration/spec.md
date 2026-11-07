## MODIFIED Requirements

### Requirement: Worktree revision cache

The daemon SHALL maintain, per task worktree, a cached git status and an opaque revision derived from the status message, its comparison, and path/size/mtime-nanosecond metadata for at most 2000 dirty status entries. Above that cap it SHALL use the summary-only hash; edits preserving both size and mtime are outside the metadata fingerprint guarantee. Filesystem changes in the worktree and HEAD/index changes in its git directory SHALL mark the cache dirty, and bursts SHALL coalesce into a single-flight refresh. The cache SHALL be keyed on the task's comparison base and SHALL be discarded when the task is deleted or archived or its worktree disappears. Registration and archive removal SHALL share a lock, and registration SHALL check the current archive state under that lock. Watch roots SHALL resolve symlinks before traversal. Watch budgets SHALL be cumulative per entry and process-wide. Darwin accounting SHALL include directory children opened implicitly by fsnotify; Linux SHALL count directory watches. Failed allocation or excess budget SHALL close the entire entry watcher and degrade it to max-age refresh. Darwin growth checks enforce an approximate budget by recount and degradation, rather than preventing each internal kqueue open.

#### Scenario: Edit changes the revision
- **WHEN** a watched dirty file changes size or mtime within the fingerprint cap, or its status summary changes
- **THEN** a `worktree.changed` event with a new revision is published

#### Scenario: Burst coalesces
- **WHEN** many writes land within the debounce window
- **THEN** at most one refresh runs per window and an unchanged resulting revision publishes nothing
#### Scenario: Overlapping writes
- **WHEN** another state arrives during an in-flight refresh
- **THEN** one follow-up refresh captures it, with at most one concurrent fetch per entry

#### Scenario: Archived read
- **WHEN** an archived task is inspected after watcher cleanup
- **THEN** no persistent watcher or cache is recreated


### Requirement: Single-file discard

The daemon SHALL restore one worktree path to HEAD, undoing staged and unstaged edits, without touching other paths. Paths not in HEAD SHALL be removed only on explicit request. Rename pairs and unmerged paths SHALL be refused, and containment SHALL be re-verified immediately before each Git mutation. Filesystem deletion SHALL use an opened worktree root handle that cannot follow a replaced parent outside the worktree.

#### Scenario: Tracked edits restored
- **WHEN** a tracked file has staged and unstaged edits and is discarded
- **THEN** it matches HEAD and other files are unchanged

#### Scenario: Escape rejected
- **WHEN** the path traverses a symlink leaving the worktree
- **THEN** it is rejected and the outside file is untouched
#### Scenario: Parent replaced during index removal
- **WHEN** a staged new file's parent is replaced by an outside symlink while Git removes the index entry
- **THEN** deletion through the opened worktree root refuses the escape and the outside file survives
