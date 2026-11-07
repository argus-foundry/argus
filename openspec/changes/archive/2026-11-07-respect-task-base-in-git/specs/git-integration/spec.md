## ADDED Requirements

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
