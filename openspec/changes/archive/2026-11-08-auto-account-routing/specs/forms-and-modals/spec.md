## MODIFIED Requirements

### Requirement: New-task form offers an account selector

The new-task form (TUI, web, macOS) SHALL show an "Account" selector listing `default` plus the configured accounts that support the selected backend, preselected from the project then global default, and labelled with the account's label and Claude signed-in identity when known; the `default` entry SHALL be labelled to indicate it is routed by usage. The selector SHALL be hidden when only `default` applies, and SHALL re-filter when the backend changes, keeping the user's pick if it still applies. A project change SHALL re-preselect that project's default. Selecting `default` SHALL submit an empty account so the daemon routes by usage headroom (a project `project_accounts` pin still applies); selecting a named account SHALL submit that name as an explicit pin. When the selector is hidden the form SHALL submit an empty value. Skill autocomplete SHALL read skills from the effective account's Claude config dir. In remote mode the TUI SHALL take account names and labels from the daemon. The selector MUST implement paste/keyboard handling consistent with the other selectors.

#### Scenario: Selector hidden with no accounts
- **WHEN** no accounts are configured
- **THEN** the form has no Account field and tab order is unchanged

#### Scenario: Default selection routes
- **WHEN** accounts `personal` and `work` exist and `default` is selected
- **THEN** submitting sends an empty account so routing picks by headroom

#### Scenario: Project default preselected
- **WHEN** `project_accounts` maps the selected project to `personal`
- **THEN** the selector starts on `personal`

#### Scenario: Named account pins
- **WHEN** the user selects `personal`
- **THEN** the form submits `personal`

#### Scenario: Backend filters the list
- **WHEN** the selected backend is codex and account `work` defines only `claude_config_dir`
- **THEN** `work` is not offered
