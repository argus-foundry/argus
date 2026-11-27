package model

import (
	"sort"

	"github.com/argus-foundry/argus/internal/db"
)

// BoardQuiet reports whether an item is excluded from human-attention counts
// and board-only jump targets: every board todo (team work) and any action or
// todo whose trigger has not been marked ready. Mirrors the store's rule.
func BoardQuiet(i *db.HeraBoardItem) bool {
	return i.DeferredAt != nil || i.Kind == "todo" || ((i.Kind == "action" || i.Kind == "todo") && i.TriggerPending)
}

// BoardAttention reports whether an item contributes to the human-attention
// unread count (unread and not quiet).
func BoardAttention(i *db.HeraBoardItem) bool { return i.AttentionUnread() }

func boardBand(i *db.HeraBoardItem) int {
	switch {
	case i.Kind == "action" && i.Blocking && !i.Resolved:
		return 0
	case BoardQuiet(i):
		return 2
	}
	return 1
}

// SortBoardItems applies the shared board order in place: unresolved blocking
// actions, then everything ordinary, then pending items and todos; ascending
// id within each band. Idempotent, so it is safe on already-ordered input.
func SortBoardItems(items []*db.HeraBoardItem) {
	sort.SliceStable(items, func(a, b int) bool {
		ba, bb := boardBand(items[a]), boardBand(items[b])
		if ba != bb {
			return ba < bb
		}
		return items[a].ID < items[b].ID
	})
}
