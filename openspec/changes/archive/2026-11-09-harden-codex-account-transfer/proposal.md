# Harden Codex account transfer

Approved by the round-one assignment. Discover the source thread before live first-run moves, validate the target before missing-source fallbacks, confine rollout access to the account sessions roots, preserve existing target history on index failure, honor fresh launch selection, and cancel queued HTTP moves. Web ENG-1082 and TUI ENG-1093 remain the UI follow-ups; Mac UI is out of scope. The shared API supplies these corrected outcomes to every client.

## Abstraction inventory

| Name | Type | Description | Contains | Depends on |
| --- | --- | --- | --- | --- |
| Codex thread | Entity | The conversation selected by task identity or capture discovery | Index row and rollout | Account home |
| Transfer readiness | Policy | Target must have a usable thread index before missing-source fallback | Login and schema checks | Account home |
| Rollout transfer | Operation | Copy a confined regular source file while preserving prior destination on failure | Rooted staging and index write | Codex thread |
| Account move | Operation | Persist account and conversation identity then select resume or fresh launch | Thread transfer and session restart | Rollout transfer |
| Task mutation admission | Policy | Canceled requests cannot acquire permission to mutate later | Context-aware task lock | HTTP context |

The existing codexmove.go mixes filesystem staging and SQL row operations. Split those into codexmove_files.go and codexmove_index.go while retaining codexTransferSession as the orchestration entry. Existing task runtime starts remain behind restartTaskSession; a named launch helper will accept explicit resume selection.
