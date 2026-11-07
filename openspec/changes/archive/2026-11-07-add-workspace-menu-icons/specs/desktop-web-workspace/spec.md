## MODIFIED Requirements

### Requirement: Rail configuration and task settings
Project Configuration gears SHALL open the existing Settings project editor directly and SHALL be hidden when no configuration action is available. Repository navigation SHALL own the only project list and Add repository entry; the Projects overview SHALL be removed. Task rows SHALL offer a hover/focus actions button left of the existing archive icon, without selecting the row. Its menu SHALL offer Task settings, explicit Pin/Unpin and copying available task ID, branch and worktree values, followed by a separated red Delete action in Active and Archived. Archive/restore SHALL remain on the rail, in task settings and on the shortcut; the header and menu archive actions SHALL be removed. Every action menu item SHALL have exactly one leading decorative inline SVG icon from the shared monochrome stroke set, in a fixed aligned column, inheriting its label color. Destructive items SHALL have red text and icons and a preceding divider. Settings navigation entries SHALL use the same icon set, including Add repository, while repository identity avatars SHALL retain their letters. Labels, accessible names and keyboard behavior SHALL remain unchanged. Native form select options SHALL retain browser rendering. Menus SHALL support keyboard dismissal, focus restoration and periodic rail refresh without losing their target.

#### Scenario: Inspect and change a captured task
- **WHEN** Task settings is chosen for a task
- **THEN** a main-pane task settings view SHALL load that captured task's DTO/raw facts and existing optional account/backend/PR metadata
- **AND** rename and workflow status SHALL use dedicated endpoints with inline failure recovery
- **AND** supported session/account/fork/delete actions SHALL use existing endpoints, with explicit confirmation for stopping, resuming, moving account or deleting
- **AND** account movement SHALL appear only for a verified Claude backend with compatible choices and SHALL never silently force a busy agent
- **AND** failed changes SHALL keep entered values; closed, disconnected or superseded views SHALL reject late results
- **AND** a newer retry SHALL cancel and supersede older reads of the same task without overwriting newer edits
- **AND** a failed details read after a confirmed mutation SHALL remain visible with recovery rather than being replaced by a successful-load message
- **AND** readonly facts SHALL NOT become generic raw-task overwrites or claims of runtime model/sandbox/branch changes
- **AND** Escape, Back and Forward SHALL restore the prior workspace; selecting another task SHALL leave settings without dispatching a pending action to the new task

#### Scenario: Consistent icons in every action menu
- **WHEN** a task, coordinator or Changes file menu is rendered, including disabled and link items
- **THEN** every item SHALL contain exactly one leading aria-hidden SVG with shared size and stroke
- **AND** destructive items SHALL inherit red for their icon and label and have a preceding separator
- **AND** light or dark color tokens and a 220px rail SHALL preserve aligned labels without clipping
- **AND** keyboard arrows, Home/End, Escape and focus restoration SHALL retain their existing behavior
