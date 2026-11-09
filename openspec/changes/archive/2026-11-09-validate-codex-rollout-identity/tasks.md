- [x] Prove the reviewed baseline accepts hard-linked credentials, copied credentials and multiply linked valid rollouts, then reject them in regression tests.
- [x] Validate opened-fd link count and native session_meta identity before copying.
- [x] Update legitimate fixtures and prove complete valid rollouts transfer.
- [x] Archive the delta and base spec with one new fix commit.

Post-commit validation is required by the round-three assignment. Run focused agent/API races and one full pre-pr under the shared lock on the committed tree. Record results under .context/pr1154/r3/ without amending the tested commit.
