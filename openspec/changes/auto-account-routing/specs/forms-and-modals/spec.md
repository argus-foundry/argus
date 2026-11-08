## MODIFIED Requirements

### Requirement: New-task form offers an account selector

The new-task form (TUI, web, macOS) SHALL show an "Account" selector listing `Auto`, `default`, plus the configured accounts that support the selected backend, labelled with the account's label and Claude signed-in identity when known. `Auto` SHALL be the initial selection (after project, backend or form reset) and SHALL submit an empty account so the daemon routes by usage headroom (a project `project_accounts` pin still applies). Choosing `default` or a named account SHALL submit that name as an explicit pin. The selector SHALL be hidden when only `default` applies and SHALL submit an empty value then, and SHALL re-filter when the backend changes, keeping the user's pick if it still applies. Skill autocomplete SHALL read skills from the effective account's Claude config dir (for `Auto`, the project's resolved default). In remote mode the TUI SHALL take account names and labels from the daemon. The selector MUST implement paste/keyboard handling consistent with the other selectors.

#### Scenario: Selector hidden with no accounts
- **WHEN** no accounts are configured
- **THEN** the form has no Account field and tab order is unchanged

#### Scenario: Auto is preselected
- **WHEN** accounts `personal` and `work` exist and the form opens
- **THEN** the selector starts on `Auto` and submitting it sends an empty account

#### Scenario: Project pin still applies to Auto
- **WHEN** `project_accounts` maps the selected project to `personal` and `Auto` is selected
- **THEN** the submitted account is empty and the daemon resolves `personal`

#### Scenario: Explicit choice pins
- **WHEN** the user selects `default`
- **THEN** the form submits `default`

#### Scenario: Backend filters the list
- **WHEN** the selected backend is codex and account `work` defines only `claude_config_dir`
- **THEN** `work` is not offered
