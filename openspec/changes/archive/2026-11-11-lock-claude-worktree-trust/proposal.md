## Why

Claude Code writes its global config under a shared directory lock. Argus must honor that lock so trust seeding cannot replace a newer login or settings update from a live session.

## What changes

- Acquire Claude's `<config>.lock` directory before reading and hold it through atomic rename, with a short bounded wait and fail-open behavior.
- Keep the lease fresh while held, refuse a write after ownership loss, and never steal or remove another writer's lock.
- Document real CLI probes: neither Claude nor Codex accepts a shared worktrees parent for a linked worktree; exact worktree or main repository trust works.

## Capabilities

### New capabilities

None.

### Modified capabilities

- `claude-worktree-trust`: Serialize config updates with live Claude writers.

## Impact

Claude trust injection and atomic writer guard; shared creation still fails open on errors across all frontends. Codex 0.167.0 still prompts with approval/sandbox bypass and with a per-launch `-c projects."<worktree>".trust_level="trusted"` override, while an entry in its temporary user config suppresses the prompt. Its account overlay preserves rather than seeds trust. **Named follow-up: Codex trust provisioning** must resolve that startup gate without editing the account's shared config.toml; this correction retains the Claude-only scope after the approved per-launch experiment failed.
