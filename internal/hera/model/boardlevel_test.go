package model

import (
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestBoardItemLevel(t *testing.T) {
	tests := []struct {
		name string
		item db.HeraBoardItem
		want BoardLevel
		jump bool
	}{
		{"blocking action", db.HeraBoardItem{Kind: "action", Blocking: true}, BoardLevelCritical, false},
		{"ready action", db.HeraBoardItem{Kind: "action"}, BoardLevelWarn, false},
		{"decision", db.HeraBoardItem{Kind: "decision"}, BoardLevelWarn, true},
		{"fyi", db.HeraBoardItem{Kind: "fyi"}, BoardLevelInfo, false},
		{"todo", db.HeraBoardItem{Kind: "todo"}, BoardLevelNone, false},
		{"pending blocking action", db.HeraBoardItem{Kind: "action", Blocking: true, TriggerPending: true}, BoardLevelNone, false},
		{"read decision", db.HeraBoardItem{Kind: "decision", Read: true}, BoardLevelNone, false},
		{"resolved blocking action", db.HeraBoardItem{Kind: "action", Blocking: true, Resolved: true}, BoardLevelNone, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Equal(t, BoardItemLevel(&tc.item), tc.want)
			testutil.Equal(t, BoardJumpable(&tc.item), tc.jump)
		})
	}
}
