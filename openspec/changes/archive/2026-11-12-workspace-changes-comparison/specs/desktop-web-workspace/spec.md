## ADDED Requirements

### Requirement: Changes comparison provenance
The workspace SHALL label status and diff using returned comparison requested_base, resolved_base, merge_base and fallback_reason. It SHALL explain local HEAD changes, branch merge-base changes and untracked contents without inferring a per-file comparison kind absent from the contract. Fallback warnings SHALL name the effective base.

#### Scenario: Adopt the merged contract
- **WHEN** A stacked task displays only the returned list, and its diff labels the same returned provenance with effective-base fallback warnings.
- **THEN** the client follows the merged daemon contract, retaining captured-task cancellation and accessible feedback
