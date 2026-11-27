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

// BoardLevel is the urgency of a coordinator's open board attention, ordered so
// the maximum over its items is the most urgent.
type BoardLevel int

const (
	BoardLevelNone     BoardLevel = iota // nothing demanding attention (quiet items never count)
	BoardLevelInfo                       // only FYIs
	BoardLevelWarn                       // an open decision or a ready non-blocking action
	BoardLevelCritical                   // an open blocking action
)

// BoardItemLevel grades one item through the shared attention predicate, so a
// quiet item (todo, trigger-pending action) or a read/resolved one is None.
func BoardItemLevel(i *db.HeraBoardItem) BoardLevel {
	if !BoardAttention(i) {
		return BoardLevelNone
	}
	switch {
	case i.Kind == "fyi":
		return BoardLevelInfo
	case i.Kind == "action" && i.Blocking:
		return BoardLevelCritical
	}
	return BoardLevelWarn
}

// BoardJumpable reports whether an item makes its coordinator a ctrl+g jump
// target: an attention item that is a decision. FYIs and actions never do, even
// blocking ones (the jump hotkey is deliberately unchanged by board kinds).
func BoardJumpable(i *db.HeraBoardItem) bool {
	return BoardAttention(i) && i.Kind == "decision"
}

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
