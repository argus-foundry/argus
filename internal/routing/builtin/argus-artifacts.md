---
tags: [argus]
audience: [shared]
---
## Claude.ai artifact publishing (argus sandboxes)

If `ARGUS_ARTIFACT_ACCOUNT` is unset, ignore this section.

When it is set, this project's claude.ai artifacts live on that account, and this session is not signed into it. Publish and update artifacts with the `artpush` CLI instead of the built-in artifact tool or the `claude-artifacts` MCP tools:

- New artifact: `artpush publish <path> --account "$ARGUS_ARTIFACT_ACCOUNT"`
- Existing artifact: `artpush publish <path> --account "$ARGUS_ARTIFACT_ACCOUNT" --update <url>`

Return the link it prints. If `artpush` fails (for example, the account is not logged in), report the error to the user, and for a missing login give the remedy `artpush login <account>`. Do not fall back to the built-in artifact tool: that publishes to the wrong account.
