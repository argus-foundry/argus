## MODIFIED Requirements

### Requirement: Move a task to another account over REST

The account move endpoint SHALL return restart_unknown with settings_applied:true, moved:true, rolled_back:false and started:false after a kick error, checked probe error, or pending restart at the response deadline. An accepted kick SHALL be settled with the same checked restart probe as runtime edits. A confirmed dead replacement SHALL return restart_failed, while a confirmed live replacement SHALL report started:true. Every persisted account move SHALL emit its settled account_changed event once. An ambiguous start SHALL preserve the cleared Hera closeout state; a definite start failure SHALL restore it. POST /account SHALL retain the existing residentGate rejection without mutations.

#### Scenario: Checked restart probe fails
- **WHEN** a kick succeeds but its checked probe fails
- **THEN** the response reports restart_unknown and keeps the target account

#### Scenario: Ambiguous start after force revive
- **WHEN** a done or failed Hera worker is force revived for an account move and start returns ErrStartAmbiguous
- **THEN** its role remains working and ready_to_close remains false
