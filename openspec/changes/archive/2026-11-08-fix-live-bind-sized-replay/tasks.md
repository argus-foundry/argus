## 1. Tests

- [x] 1.1 Real-bytes fixture (last 20KB of the garbled coordinator log) through `Draw`: one footer, intact message; red without the fix
- [x] 1.2 `readLiveRebuildHistoryAt` origin is the log offset, -1 for ring-only

## 2. Implementation

- [x] 2.1 `readLiveRebuildHistoryAt` + sized live feed in renderLive's full-replay branch

## 3. Docs

- [x] 3.1 Gotcha + knowledge index
- [x] 3.2 Archive the change (merge delta into `openspec/specs/terminal-rendering/spec.md`)
