## MODIFIED Requirements

### Requirement: Composer attachments through the existing upload API
The client SHALL accept file drops on the composer and selected task's agent panel, and image paste into the composer, using existing multipart task uploads. File drags anywhere in the window SHALL NOT navigate or open a tab; non-file drags SHALL retain native behavior. Uploads and attachment chips SHALL belong to the task selected when accepted, remain in memory only, and show filenames, image previews, pending/error state, removal and explicit retry. No daemon or original static asset changes are included.

#### Scenario: Drop or paste an attachment
- **WHEN** files are dropped on an eligible task's composer or agent panel, or an image is pasted into the composer
- **THEN** the client SHALL upload with multipart `files` parts without setting Content-Type, with a timeout below the existing 300 second read limit
- **AND** a file drag SHALL highlight its target, with a clear disabled message when no task can take input
- **AND** plain-text paste SHALL retain the accepted explicit insertion behavior without submitting pasted newlines

#### Scenario: Switch task or send attachments
- **WHEN** task selection changes during an upload
- **THEN** its resulting paths and chips SHALL remain associated with the original task
- **AND** explicit send SHALL include ready attachment paths as space-separated tokens, escaping whitespace, quotes, backslashes and shell-significant characters with backslashes as terminal file drag-and-drop does, leaving simple paths unchanged and preserving the user text without an upload-triggered submission
- **AND** removed chips SHALL NOT contribute paths to subsequent sends
- **AND** uploads or errors SHALL require completion, removal or explicit retry before sending

#### Scenario: Remove or abandon an upload
- **WHEN** a chip is removed, successfully sent, or the client disconnects
- **THEN** its local image URL SHALL be revoked and obsolete upload callbacks SHALL be ignored
- **AND** canceled or timed-out uploads SHALL be described as potentially completed on the server
- **AND** removing a chip SHALL NOT imply deletion of an already uploaded file
