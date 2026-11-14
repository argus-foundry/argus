## ADDED Requirements

### Requirement: `>` on the resident row moves the resident between accounts

In the resident tab, `>` on the resident's own row SHALL open the account picker used for tasks, listing `auto` and every Claude-capable account (current one marked, usage shown) and no backend-handoff rows, and SHALL move the resident through the daemon's resident move (carrying its conversation) on selection. A busy session SHALL show the same confirm-to-interrupt flow as tasks. `>` on a worker row SHALL stay inert in this tab (resident workers are moved by the resident or through the existing task paths). Success and refusal SHALL show a status-bar notice and be logged under `[account]`. This account move SHALL NOT be treated as one of the structure-changing actions the tab keeps inert; those concern Hera re-parenting and spawning.

#### Scenario: Picker on the resident row

- **WHEN** the resident row is selected and the human presses `>`
- **THEN** the picker shows `auto` and the Claude accounts only, and choosing one moves the resident and flashes a notice

#### Scenario: No handoff rows

- **WHEN** the picker opens for the resident
- **THEN** no Codex or other-CLI handoff rows are listed

### Requirement: Settings edits the resident's accounts as an ordered multi-select list

Settings -> Resident SHALL show `Accounts` as one row summarizing the ordered list (for example `personal, default, auto`; `default` when unset). Enter on the row SHALL open an editor listing every Claude-capable account plus `<auto>`, each with a checkbox, its position in the list when checked, and its current usage percentages when known. Space SHALL toggle the entry under the cursor; `K` and `J` SHALL move a checked entry up or down; `<auto>` SHALL always sort last; Enter SHALL save and Esc SHALL cancel. The saved order SHALL be what the daemon uses (stored as the `resident.accounts` database key). When config.toml sets `resident.accounts` the row SHALL be marked `(config.toml)` and Enter SHALL only explain that it is read-only. Editing is local only; `--remote` shows the row read-only. An empty selection SHALL save as the default list.

#### Scenario: Reorder and save

- **WHEN** the editor opens with `default` checked and the user checks `personal`, moves it above `default`, and presses Enter
- **THEN** `resident.accounts` is saved as `personal, default` and the row shows it

#### Scenario: Auto sorts last

- **WHEN** `<auto>` is checked along with two accounts
- **THEN** it is listed after them regardless of cursor moves

#### Scenario: config.toml wins

- **WHEN** config.toml defines `[resident] accounts`
- **THEN** the row shows `(config.toml)` and Enter opens no editor
