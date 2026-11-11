## 1. Approval
- [x] Obtain approval for the proposed behavior.

## 2. Implementation
- [x] Add failing regression tests for Pi dash-prefixed prompts, including Markdown frontmatter and shell metacharacters.
- [x] Normalize only dash-prefixed Pi positional prompts with a leading newline using family-owned behavior; log normalization without prompt content.
- [x] Verify ordinary prompts, explicit prompt flags, resumes, and other families are unchanged.
- [x] Document the Pi parser gotcha and run targeted tests and coverage.
- [x] Archive the change and merge its requirements into the base spec in the same PR.

## Verification results
- Regression tests failed before implementation and passed after it.
- Shell-decoded argv tests cover frontmatter, long/short options, single dash, literal shell metacharacters, ordinary/whitespace-leading prompts, and @file inclusion.
- Explicit prompt flags, other families, empty prompts, and resume delivery remain unchanged; normalization logging is asserted.
- make test-cover: PASS (full race suite; filtered coverage 89.3%).
- go vet ./internal/agent ./internal/daemon: PASS.
- Supervisor spawn surface bumped to 46 with the corresponding digest; stream surface unchanged.
- Solo broad review and adversarial test review: no outstanding findings; the pre-fix red run proves the tests catch removal of normalization.
