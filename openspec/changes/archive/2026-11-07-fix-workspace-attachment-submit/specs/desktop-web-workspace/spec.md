## MODIFIED Requirements

### Requirement: Attachment submission observes recipient readiness
Attachment sends SHALL write one closed paste, allow output to settle within a bounded wait, and send exactly one standalone CR. Literal image paths SHALL NOT prevent submission. If output never settles, the wait SHALL expire and allow the CR while the captured session remains live. Successful daemon acceptance of CR SHALL clear the captured browser draft and attachments without requiring screen acknowledgment. The client SHALL NOT retry CR or paste automatically. Cancellation and input failures before acceptance SHALL retain the captured draft; disconnect retains its existing clearing behavior. Plain-text sending SHALL retain its existing behavior.

#### Scenario: Slow image ingestion
- **WHEN** an attachment paste finishes asynchronously, either as literal text and paths or an image marker
- **THEN** the client SHALL wait for output settling within a bounded interval before exactly one separate CR
- **AND** it SHALL clear the browser draft and attachments after the daemon accepts CR

#### Scenario: Swallowed Enter
- **WHEN** the daemon accepts CR but the terminal remains unchanged or has an unsupported layout
- **THEN** the browser SHALL clear its accepted draft and attachments
- **AND** the client SHALL NOT send another CR or repeat the paste

#### Scenario: Enforce the development proxy trust boundary
- **WHEN** requests reach the loopback development server
- **THEN** invalid Host and cross-site fetch requests SHALL be rejected, local navigation SHALL remain supported and static assets SHALL follow the allowlist
- **AND** proxy forwarding SHALL strip cookies, Set-Cookie and hop-by-hop headers while preserving required API response headers

#### Scenario: Bound registered artifact previews
- **WHEN** an artifact path traverses outside its manifest prefix or its preview exceeds the preview size cap
- **THEN** the client SHALL reject it and cancel an oversized streaming read, including without Content-Length
- **AND** permitted bounded previews SHALL preserve exact bytes and MIME type

#### Scenario: Open a push attention target safely
- **WHEN** the workspace push worker receives a notification click with a task identifier
- **THEN** it SHALL open an encoded same-origin task URL without interpreting the identifier as a URL or credential query
- **AND** malformed push data SHALL use safe notification defaults and the worker SHALL not intercept fetches

#### Scenario: Refuse multiline input in a plain shell
- **WHEN** composed text contains multiple lines and the recipient lacks bracketed paste mode
- **THEN** sending SHALL fail before writing any input bytes
- **AND** a supported bracketed paste recipient SHALL receive protected text followed by a separate submit

#### Scenario: Preserve schedule edits and unavailable measurements
- **WHEN** an existing one-shot schedule is edited without changing its cadence
- **THEN** its request SHALL omit unchanged cadence and preserve the existing one-shot time
- **AND** System measurements SHALL distinguish unavailable values from measured zero
