## 1. Skill

- [ ] 1.1 Add failing expectations for `hera-remind-me` to the builtin skill name lists in `internal/skills/builtin_test.go` (and description assertion)
- [ ] 1.2 Write `internal/skills/builtin/hera-remind-me/SKILL.md`: frontmatter (name, description scoped to the coordinator board, allowed-tools), role detection, item drafting rules, coordinator list-then-update-or-post flow, worker/freelance relay via `hera_send`, plain-task stop
- [ ] 1.3 Re-record any builtin skill digest fixtures the new file affects

## 2. Docs

- [ ] 2.1 Mention `/hera-remind-me` in the Hera skill's board guidance line so coordinators recognize relayed requests
- [ ] 2.2 Update the README Reference appendix if it lists builtin skills

## 3. Verify and archive

- [ ] 3.1 `make pre-pr` passes
- [ ] 3.2 Archive the change into `openspec/specs/coordinator-board/spec.md`
