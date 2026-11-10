## MODIFIED Requirements

### Requirement: A task can be moved to another account

The opened source rollout SHALL have exactly one filesystem link according to fstat. Before copying, its first bounded JSONL line SHALL be a native session_meta record whose payload id matches the indexed thread, with matching indexed cwd and a creation timestamp. Invalid content and multiply linked files SHALL be rejected with the existing account error before destination publication, leaving account settings, source and target files and thread rows unchanged. The validated descriptor SHALL be rewound before copying the entire rollout. Existing non-symlink and opened-identity checks remain required.

#### Scenario: Credentials hard-linked at a rollout path
- **WHEN** the source rollout is a hard link to auth.json
- **THEN** the move is refused without copying credential bytes or changing account state or indexes

#### Scenario: Copied credentials at a rollout path
- **WHEN** a source rollout has one link but contains a copied credential JSON document
- **THEN** session_meta validation refuses the move before writing the target

#### Scenario: Legitimate rollout
- **WHEN** a singly linked rollout has valid metadata for the indexed thread
- **THEN** the entire file, including its header, is copied
