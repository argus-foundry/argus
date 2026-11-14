## ADDED Requirements

### Requirement: `>` on the resident row moves the resident between accounts

In the resident tab, `>` on the resident's own row SHALL open the account picker used for tasks, listing `auto` and every Claude-capable account (current one marked, usage shown) and no backend-handoff rows, and SHALL move the resident through the daemon's resident move (carrying its conversation) on selection. A busy session SHALL show the same confirm-to-interrupt flow as tasks. `>` on a worker row SHALL stay inert in this tab (resident workers are moved by the resident or through the existing task paths). Success and refusal SHALL show a status-bar notice and be logged under `[account]`. This account move SHALL NOT be treated as one of the structure-changing actions the tab keeps inert; those concern Hera re-parenting and spawning.

#### Scenario: Picker on the resident row

- **WHEN** the resident row is selected and the human presses `>`
- **THEN** the picker shows `auto` and the Claude accounts only, and choosing one moves the resident and flashes a notice

#### Scenario: No handoff rows

- **WHEN** the picker opens for the resident
- **THEN** no Codex or other-CLI handoff rows are listed
