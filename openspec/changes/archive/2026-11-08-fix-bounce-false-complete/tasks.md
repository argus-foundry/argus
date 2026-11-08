## 1. Tests

- [x] 1.1 Reproduce restart → probe says dead → complete → reaped for a coordinator
- [x] 1.2 Exit-info query and status probe unit tests; reaper guard tests

## 2. Implementation

- [x] 2.1 Exit-info query answers stream-lost when nothing is recorded
- [x] 2.2 Status probe surfaces failed supervisor query as an error
- [x] 2.3 Reaper keeps sessions of tasks with a live coordinator/freelance binding, logging the binding
- [x] 2.4 Bump stream surface to 15, re-record digest

## 3. Docs

- [x] 3.1 Update the known-unfixed gotcha and the knowledge index
