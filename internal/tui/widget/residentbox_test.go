package widget

import (
	"testing"

	"github.com/drn/argus/internal/testutil"
	"github.com/gdamore/tcell/v2"
)

func TestResidentBox_HeightMarkerAndCallback(t *testing.T) {
	b := NewResidentBox()
	calls := 0
	b.OnHeightChange = func() { calls++ }
	testutil.Equal(t, b.DesiredHeight(), 0)

	b.Set("Solo", true, false, "hi", false)
	testutil.Equal(t, b.DesiredHeight(), 1)
	testutil.Equal(t, b.Marker(), "●")
	testutil.Equal(t, calls, 1)

	b.Set("Solo", true, true, "hi", false) // waiting outranks unread, no height change
	testutil.Equal(t, b.Marker(), "?")
	testutil.Equal(t, calls, 1)

	b.Set("Solo", true, true, "hi", true) // suppressed on the resident tab
	testutil.Equal(t, b.DesiredHeight(), 0)
	testutil.Equal(t, calls, 2)

	b.Set("Solo", false, false, "", false) // quiet
	testutil.Equal(t, b.DesiredHeight(), 0)
	testutil.Equal(t, calls, 2)
}

func TestResidentBox_Draw(t *testing.T) {
	scr := tcell.NewSimulationScreen("")
	testutil.NoError(t, scr.Init())
	scr.SetSize(30, 3)
	b := NewResidentBox()
	b.SetRect(0, 0, 30, 1)
	b.Set("Solo", false, true, "needs a yes", false)
	b.Draw(scr)
	scr.Show()
	cells, w, _ := scr.GetContents()
	got := ""
	for i := 0; i < w; i++ {
		got += string(cells[i].Runes)
	}
	testutil.Contains(t, got, "? Solo: needs a yes")

	b.SetRect(0, 0, 8, 1)
	b.Draw(scr)
	scr.Show()
	cells, _, _ = scr.GetContents()
	testutil.Equal(t, string(cells[7].Runes), "…")
}
