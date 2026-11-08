## ADDED Requirements

### Requirement: Independent coalesced context refresh
Context measurement reads SHALL publish independently of auxiliary account, backend and bridge usage reads. Same-session context events SHALL retain valid displayed measurements while refetching, debounce bursts and share overlapping reads. Context events SHALL NOT refetch auxiliary usage. Auxiliary data SHALL retain its existing foreground cadence and explicit-open refresh. Legacy Hera metadata SHALL be fetched only for unsupported context routes. Task or session replacement SHALL still cancel old reads and clear prior measurements immediately. Supported empty or failed context reads SHALL still clear measurements. Shared reads SHALL have a bounded deadline so a hung request cannot prevent later refreshes.

#### Scenario: Slow accounts
- **WHEN** an account request is still pending and a context result arrives
- **THEN** the context categories publish immediately without awaiting the account read

#### Scenario: Burst and overlapping triggers
- **WHEN** ten same-session context events arrive twenty milliseconds apart
- **THEN** they coalesce into a bounded context-only refresh without blanking displayed categories
- **AND** overlapping open and poll refreshes share in-flight reads
