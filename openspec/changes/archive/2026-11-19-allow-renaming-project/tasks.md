## 1. DB
- [x] 1.1 `RenameProject` transaction + `ErrProjectExists` (tests: all refs rewritten, collision, missing, rollback)
## 2. REST
- [x] 2.1 `new_name` on `handleUpdateProject`; 404/409 mapping; event emit; tests
- [x] 2.2 `apistore.Store.RenameProject` + `store.Store` interface + assert
## 3. TUI
- [x] 3.1 Editable name in `ProjectForm` edit mode; route changed name via rename; smoke test
## 4. Docs
- [x] 4.1 README REST/keybinding table touch, gotcha note (worktree dirs not moved), archive change in this PR
