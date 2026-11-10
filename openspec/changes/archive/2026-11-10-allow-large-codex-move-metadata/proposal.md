# Allow large native Codex move metadata

Approved extra narrow fix round. Account moves currently inherit the context reader's 64 KiB first-record budget, rejecting legitimate session_meta containing large base instructions or dynamic tools. Use a 4 MiB move-specific budget while keeping the existing context-reader default unchanged. Share the metadata validator with an explicit budget parameter, retaining its identity checks. Link count, descriptor ownership, rewind and copying remain unchanged.

The domain concept is the validation budget for Codex move metadata. This adds no endpoint, frontend UI, supervisor behavior or protocol. Existing Web ENG-1082, TUI ENG-1093 and Mac UI boundaries remain unchanged.
