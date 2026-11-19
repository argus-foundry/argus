## 1. Page behavior

- [x] 1.1 Add failing tests for theme persistence, throwing storage, safe links, punctuation, code and hostile text.
- [x] 1.2 Implement pre-paint theme restoration and a visible selector; extend DOM text rendering to safe links across decision fields.
- [x] 1.3 Bump the service-worker version and document non-obvious invariants.

## 2. Verification and delivery

- [x] 2.1 Verify both themes at phone width in a real browser, including all text fields, persistence and credential/request boundaries.
- [x] 2.2 Review the diff and tests; run make pre-pr once with approved serial timeout recovery if needed.
- [x] 2.3 Archive the change, validate all specs strictly and prepare the reviewed commit for PR delivery.
