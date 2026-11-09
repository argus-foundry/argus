package hera

import (
	"testing"

	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/testutil"
)

func TestResidentReturnPreservesAttentionExcursion(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := memDB(t)
	orch := seedOrch(t, d, "origin")
	seedBoundRole(t, d, orch, "coord", db.HeraKindCoordinator, "origin-task")
	p := NewHeraPage(d)
	p.Refresh()
	testutil.True(t, p.SelectOrch(orch))
	p.rail.EnsureExcursionArmed()
	attention := p.rail.excursion
	p.focus.SetRegion(FocusCoord)
	p.focus.ToggleFullscreen()
	snapshot := p.CaptureReturn()
	p.rail.ToggleCollapse()
	p.focus.ToRail()
	var focused Focus
	p.OnFocusChange = func(f Focus) { focused = f }
	testutil.True(t, p.RestoreReturn(snapshot))
	testutil.Equal(t, p.rail.currentRef(), -orch)
	testutil.Equal(t, p.focus.State(), FocusCoord)
	testutil.Equal(t, focused, FocusCoord)
	testutil.True(t, p.focus.Fullscreen())
	testutil.True(t, p.rail.excursion == attention)
	testutil.True(t, p.rail.RestoreExcursion())
}

func TestResidentReturnEmptyAndAbsentSnapshots(t *testing.T) {
	p := NewHeraPage(nil)
	testutil.False(t, p.RestoreReturn(nil))
	testutil.False(t, p.RestoreReturn(&ReturnSnapshot{}))
	snapshot := p.CaptureReturn()
	testutil.True(t, p.RestoreReturn(snapshot))
	snapshot.rail.selRef = -999
	testutil.False(t, p.RestoreReturn(snapshot))
}
