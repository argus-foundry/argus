package hera

import (
	"reflect"
	"testing"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/testutil"
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
	before := make(map[int64]bool, len(p.rail.collapsed))
	for k, v := range p.rail.collapsed {
		before[k] = v
	}
	p.rail.ToggleCollapse()
	testutil.False(t, reflect.DeepEqual(before, p.rail.collapsed))
	p.focus.ToRail()
	var focused Focus
	p.OnFocusChange = func(f Focus) { focused = f }
	testutil.True(t, p.RestoreReturn(snapshot))
	testutil.Equal(t, p.rail.currentRef(), -orch)
	testutil.Equal(t, p.focus.State(), FocusCoord)
	testutil.Equal(t, focused, FocusCoord)
	testutil.True(t, p.focus.Fullscreen())
	testutil.DeepEqual(t, p.rail.collapsed, before)
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

func TestResidentReturnFocusMismatchRestoresBestEffort(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d := memDB(t)
	orch := seedOrch(t, d, "origin")
	p := NewHeraPage(d)
	p.Refresh()
	testutil.True(t, p.SelectOrch(orch))
	snapshot := p.CaptureReturn()
	snapshot.focus = FocusCoord
	p.focus.SetCoordPresent(false) // the coordinator pane is no longer in the layout
	snapshot.fullscreen = true
	testutil.True(t, p.RestoreReturn(snapshot))
	testutil.Equal(t, p.rail.currentRef(), -orch)
	testutil.Equal(t, p.focus.State(), FocusRail)
}
