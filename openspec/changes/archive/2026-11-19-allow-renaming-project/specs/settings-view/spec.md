## ADDED Requirements

### Requirement: Rename project from the edit form

The project edit form SHALL allow editing the name. Saving a changed name SHALL rename the project (with all references) instead of creating a second project.

#### Scenario: Rename in form
- **WHEN** the user edits a project's name and saves
- **THEN** the project appears under the new name only, and its tasks follow it

#### Scenario: Name collision
- **WHEN** the new name matches another project
- **THEN** the form shows an error and stays open
