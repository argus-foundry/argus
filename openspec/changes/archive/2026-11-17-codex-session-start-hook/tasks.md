## 1. Tests

- [x] 1.1 Cover SetSessionIDIfEmpty, RecordHookSessionID, the REST endpoint, the hook command, BuildCmd flag placement, and Codex resume-time recovery.

## 2. Implementation

- [x] 2.1 Fill-only DB setter, agent validator, REST endpoint, `argus codex-session-hook`, family `SessionStartHookFlag`.
- [x] 2.2 Resume-time recovery for capture-once families; run it before the resume decision; account moves opt out.
- [x] 2.3 Bump spawn surface to 54; update gotchas and README.
