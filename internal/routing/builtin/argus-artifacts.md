---
tags: [argus]
audience: [shared]
---
## Claude.ai artifact publishing (argus sandboxes)

If `ARGUS_ARTIFACT_ACCOUNT` is unset, ignore this section.

When it is set, this project's claude.ai artifacts live on that account, and this session is not signed into it. Publish and update artifacts with the Argus `artifact_publish` MCP tool instead of the built-in artifact tool or the `claude-artifacts` MCP tools:

- New artifact: call `artifact_publish` with `path` (a self-contained .html file in your worktree or a temp directory), `title`, `account` set to `$ARGUS_ARTIFACT_ACCOUNT`, and your task `id` (`$ARGUS_TASK_ID`) or `cwd`.
- Existing artifact: the same call, plus `update` set to the artifact's link.

On `status: "published"`, give the user the `url`.

On `status: "unavailable"`, the home account can't publish right now; Argus saved the page as the task artifact named in `artifact` and listed `available_accounts`. Ask the user with your question tool which of these to do:

1. Publish to an available account: one option per account in `available_accounts`; call `artifact_publish` again with that `account`.
2. Open in the browser: call `artifact_open` with the `artifact` file name.
3. Reveal in Finder: call `artifact_open` with the file name and `reveal: true`.
4. Park it for later: do nothing more. Tell the user the saved artifact's name and that they can publish it later with `argus artifact publish <file> --account $ARGUS_ARTIFACT_ACCOUNT`.

If `available_accounts` is empty, leave out the first option. If `artifact_publish` fails with an error, report the error to the user.

Never fall back to the built-in artifact tool on your own: that publishes to the wrong account.
