## MODIFIED Requirements

### Requirement: Argus-only Codex builtin skill materialization

The isolated Codex home SHALL link the source home's existing configuration, authentication, session files, bundled system skills, and user-installed skills, but SHALL keep `app-server-daemon` as a real directory owned by that overlay. It SHALL NOT symlink the source home's daemon state into the overlay. Before launching Codex, materialization SHALL reconcile an existing overlay symlink at `app-server-daemon` into a real directory without changing the source directory. A pre-existing non-symlink entry SHALL be preserved if replacement is needed. The rest of this requirement remains as specified in the base spec.

#### Scenario: Source daemon state exists

- **WHEN** Codex materialization runs with a source home containing an `app-server-daemon` directory
- **THEN** the Argus overlay contains its own real `app-server-daemon` directory, and the source directory is unchanged

#### Scenario: Existing overlay has a daemon state symlink

- **WHEN** Codex materialization runs with an overlay whose `app-server-daemon` links to the source home
- **THEN** that symlink is replaced by a real directory before Argus launches Codex, without removing source daemon state

#### Scenario: Separate account overlays

- **WHEN** Codex materialization runs for two different account homes
- **THEN** each overlay has a distinct real `app-server-daemon` directory
