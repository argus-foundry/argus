## ADDED Requirements

### Requirement: Workspace document isolation
The optional workspace proxy SHALL apply Content-Security-Policy sandbox without permissions and X-Content-Type-Options nosniff to every daemon API response, preserving existing upstream CSP restrictions. Workspace HTML shells SHALL deny framing with frame-ancestors 'none' and X-Frame-Options DENY. These requirements apply to the optional Node workspace only; daemon, TUI and macOS behavior remains unchanged.

#### Scenario: Navigate to active raw artifact content
- **WHEN** a user follows a raw artifact link, including HTML or SVG, with a remembered workspace token
- **THEN** the document SHALL NOT execute scripts or access workspace origin storage
- **AND** fetched Review previews and artifact downloads SHALL retain their bytes and MIME types

#### Scenario: Frame the remembered workspace from another loopback port
- **WHEN** another same-site origin embeds a workspace HTML shell
- **THEN** the browser SHALL refuse to display the shell in that frame
