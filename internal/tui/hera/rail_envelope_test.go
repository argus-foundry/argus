package hera

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/argus-foundry/argus/internal/db"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/tui/theme"
)

// envelopeModel builds a one-coordinator model whose BoardLevel/BoardJump are
// derived from items exactly the way BuildModel derives them.
func envelopeModel(items ...*db.HeraBoardItem) heramodel.Model {
	m := twoOrchModel()
	c := &m.Active[0].Roles[0]
	c.BoardItems = items
	c.BoardUnread, c.BoardLevel, c.BoardJump = 0, heramodel.BoardLevelNone, 0
	for _, i := range items {
		if heramodel.BoardAttention(i) {
			c.BoardUnread++
		}
		if l := heramodel.BoardItemLevel(i); l > c.BoardLevel {
			c.BoardLevel = l
		}
		if heramodel.BoardJumpable(i) {
			c.BoardJump++
		}
	}
	return m
}

func TestRailEnvelopeColors(t *testing.T) {
	tests := []struct {
		name  string
		items []*db.HeraBoardItem
		glyph rune
		color tcell.Color
	}{
		{"blocking action is red", []*db.HeraBoardItem{{Kind: "decision"}, {Kind: "action", Blocking: true}}, 0xf01ee, theme.ColorError},
		{"decision is orange", []*db.HeraBoardItem{{Kind: "fyi"}, {Kind: "decision"}}, 0xf01ee, theme.ColorInProgress},
		{"ready action is orange", []*db.HeraBoardItem{{Kind: "fyi"}, {Kind: "action"}}, 0xf01ee, theme.ColorInProgress},
		{"fyi only is blue", []*db.HeraBoardItem{{Kind: "fyi"}, {Kind: "todo"}}, 0xf01ee, theme.ColorInReview},
		{"todo and pending action are quiet", []*db.HeraBoardItem{{Kind: "todo"}, {Kind: "action", Blocking: true, TriggerPending: true}}, heraIconCoord, 0},
		{"empty board", nil, heraIconCoord, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRail()
			r.boardAuto = true
			m := envelopeModel(tc.items...)
			r.SetModel(m)
			sim := tcell.NewSimulationScreen("UTF-8")
			testutil.NoError(t, sim.Init())
			t.Cleanup(sim.Fini)
			sim.SetSize(60, 4)
			r.drawOrchRow(sim, 0, 0, 60, railRow{orch: &m.Active[0]}, false)
			sim.Show()
			glyph, style, _ := sim.Get(4, 0)
			testutil.Equal(t, glyph, string(tc.glyph))
			want := theme.StyleProject
			if tc.color != 0 {
				want = want.Foreground(tc.color)
			}
			testutil.Equal(t, style, want)
		})
	}
}

// A blocking action alone colors the envelope but is never a ctrl+g target.
func TestBoardJumpIgnoresActions(t *testing.T) {
	tests := []struct {
		name   string
		items  []*db.HeraBoardItem
		target bool
	}{
		{"blocking action alone", []*db.HeraBoardItem{{Kind: "action", Blocking: true}}, false},
		{"ready action alone", []*db.HeraBoardItem{{Kind: "action"}}, false},
		{"decision", []*db.HeraBoardItem{{Kind: "decision"}}, true},
		{"decision with blocking action", []*db.HeraBoardItem{{Kind: "decision"}, {Kind: "action", Blocking: true}}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := NewRail()
			r.boardAuto = true
			m := envelopeModel(tc.items...)
			r.SetModel(m)
			_, ok := r.nextNeedsInputRow()
			testutil.Equal(t, ok, tc.target)
		})
	}
}
