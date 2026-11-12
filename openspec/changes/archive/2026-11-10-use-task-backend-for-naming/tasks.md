## Implementation (approved)
- [x] Identify supported non-interactive utility invocations for registered backend families and their isolation/authentication requirements.
- [x] Add failing tests for resolved backend/account propagation, non-Claude invocation, and unsupported/unavailable providers without Claude fallback.
- [x] Declare naming capabilities in the family registry and implement backend-aware name generation.
- [x] Wire the resolved backend/account through the shared creation and background rename path.
- [x] Preserve and test validation, retry/deadline, explicit-name protection, user-rename CAS, and deletion handling.
- [x] Update backend-aware uxlog diagnostics, relevant documentation, and non-obvious gotchas.
- [x] Run targeted tests, coverage, and the full required gate before any PR push. `make pre-pr` passed: filtered coverage 89.3%, all 741 workspace tests passed; Claude/Codex naming and isolation covered by hermetic tests plus a live Codex Luna smoke test.
- [x] Merge the spec delta into the base spec and archive this change in the same PR.
