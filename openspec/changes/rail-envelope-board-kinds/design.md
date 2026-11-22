## Decisions

- **Reuse the predicate.** A per-item level is derived only for items passing `BoardAttention`; quiet and read/resolved items grade to none. The coordinator's level is the maximum over its items, stamped on `RoleView.BoardLevel` during model build.
- **Theme colors.** Red = existing `ColorError`, orange = `ColorInProgress` (unchanged hue), blue = `ColorInReview`. No new theme color.
- **Jump gate.** `BoardUnread` counts every attention item including actions, so gating the jump on it would make a blocking action alone a jump target. The jump now gates on `RoleView.BoardJump` (unread decisions + FYIs), preserving pre-action-kind targeting.
