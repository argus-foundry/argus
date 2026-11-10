# Validate Codex rollout identity

Approved final fix round. A regular, non-symlink file can still be a hard link to credentials. The existing source-file identity policy gains two independent requirements: opened fstat link count exactly one, and a bounded first JSONL session_meta record matching the indexed thread. Reuse codexctx.ValidateThreadRollout and its native identity shape, including indexed cwd and creation timestamp. Reset the checked descriptor before copying so the entire conversation is retained.

The domain concepts remain account transfer, opened rollout identity and indexed Codex thread. These checks precede destination publication, preserving failed-move state. No endpoint, frontend, supervisor or protocol change. Existing Web ENG-1082, TUI ENG-1093 and Mac UI boundaries remain unchanged.
