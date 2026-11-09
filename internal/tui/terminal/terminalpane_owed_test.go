package terminal

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/app/agentview"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/uxlog"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
)

func owedPane(t *testing.T, w, h int, owed *bool) (*TerminalPane, tcell.SimulationScreen) {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	tp := NewTerminalPane()
	tp.SetTaskID("owed")
	tp.SetOwedFunc(func(taskID string, _ agentview.TerminalAdapter, cols int) (bool, string) {
		if *owed {
			return true, ""
		}
		return false, "width-returned"
	}, func() string { return "ctrl+n" })
	tp.SetSession(&mockAdapter{alive: true, ptyCols: w - 2, ptyRows: h - 2})
	screen := tcell.NewSimulationScreen("UTF-8")
	testutil.NoError(t, screen.Init())
	t.Cleanup(screen.Fini)
	screen.SetSize(w, h)
	tp.SetRect(0, 0, w, h)
	return tp, screen
}

func TestTerminalPane_OwedNotice_TopBorderRight(t *testing.T) {
	owed := true
	tp, screen := owedPane(t, 100, 24, &owed)
	tp.SetBorderTitle(" Coordinator ")
	tp.Draw(screen)
	top := screenRowText(screen, 0, 100)
	testutil.Contains(t, top, "Coordinator")
	testutil.Contains(t, top, "width changed · ctrl+n re-render")
	// Right-aligned: notice sits after the title, ending at the last inner column.
	testutil.True(t, strings.Index(top, "width changed") > strings.Index(top, "Coordinator"))
	testutil.True(t, strings.HasSuffix(strings.TrimRight(top, "─┐ "), "re-render") || strings.Contains(top, "re-render "))

	owed = false
	tp.Draw(screen) // no event yet: the stored state is painted, nothing re-evaluated
	testutil.Contains(t, screenRowText(screen, 0, 100), "width changed")
	tp.InvalidateOwed() // e.g. the setting flipped
	tp.Draw(screen)
	testutil.False(t, strings.Contains(screenRowText(screen, 0, 100), "width changed"))
}

func TestTerminalPane_OwedNotice_PrecedenceAndTruncation(t *testing.T) {
	owed := true
	tp, screen := owedPane(t, 100, 24, &owed)
	tp.SetRerenderingForTask("owed", true)
	tp.Draw(screen)
	top := screenRowText(screen, 0, 100)
	testutil.Contains(t, top, "Re-rendering…")
	testutil.False(t, strings.Contains(top, "width changed"))
	tp.SetRerenderPendingForTask("owed")
	tp.Draw(screen)
	top = screenRowText(screen, 0, 100)
	testutil.Contains(t, top, "Re-render pending")
	testutil.False(t, strings.Contains(top, "width changed"))
	tp.SetRerenderingForTask("owed", false)

	// Narrow pane: the long form would collide with the title, so a shorter
	// form (or nothing) is drawn, but the title always stays intact.
	narrowOwed := true
	tp2, screen2 := owedPane(t, 34, 12, &narrowOwed)
	tp2.Draw(screen2)
	top = screenRowText(screen2, 0, 34)
	testutil.Contains(t, top, "Agent")
	testutil.False(t, strings.Contains(top, "width changed"))
	// Very narrow: omitted entirely, title intact, no panic.
	tinyOwed := true
	tp3, screen3 := owedPane(t, 20, 8, &tinyOwed)
	tp3.Draw(screen3)
	testutil.Contains(t, screenRowText(screen3, 0, 20), "Agent")
}

func TestTerminalPane_OwedNotice_ClickRunsRerender(t *testing.T) {
	owed := true
	tp, screen := owedPane(t, 100, 24, &owed)
	clicks := 0
	tp.OnOwedClick = func() { clicks++ }
	tp.Draw(screen)
	top := screenRowText(screen, 0, 100)
	runes := []rune(top)
	idx := -1
	needle := []rune("width changed")
	for i := range runes {
		if i+len(needle) <= len(runes) && string(runes[i:i+len(needle)]) == string(needle) {
			idx = i
			break
		}
	}
	testutil.True(t, idx >= 0)
	handler := tp.MouseHandler()
	focused := false
	setFocus := func(tview.Primitive) { focused = true }
	click := func(x, y int) {
		ev := tcell.NewEventMouse(x, y, tcell.Button1, 0)
		handler(tview.MouseLeftDown, ev, setFocus)
	}
	click(idx+2, 0) // on the notice
	testutil.Equal(t, clicks, 1)
	testutil.True(t, focused)
	click(idx+2, 5) // pane body: no re-render
	testutil.Equal(t, clicks, 1)
	click(3, 0) // title area of the border: no re-render
	testutil.Equal(t, clicks, 1)
}

func TestTerminalPane_OwedNotice_TransitionLogsOnce(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "ux.log")
	uxlog.Close()
	testutil.NoError(t, uxlog.Init(logPath))
	t.Cleanup(uxlog.Close)
	owed := true
	tp, screen := owedPane(t, 100, 24, &owed)
	for range 5 {
		tp.Draw(screen) // many frames, one transition
	}
	owed = false
	tp.InvalidateOwed()
	for range 5 {
		tp.Draw(screen)
	}
	b, _ := os.ReadFile(logPath)
	log := string(b)
	testutil.Equal(t, strings.Count(log, "[rerender] owed notice shown"), 1)
	testutil.Equal(t, strings.Count(log, "[rerender] owed notice cleared"), 1)
	testutil.Contains(t, log, "reason=width-returned")
}

func TestTerminalPane_OwedNotice_ClearsOnRebind(t *testing.T) {
	logPath := filepath.Join(t.TempDir(), "ux.log")
	uxlog.Close()
	testutil.NoError(t, uxlog.Init(logPath))
	t.Cleanup(uxlog.Close)
	owed := true
	tp, screen := owedPane(t, 100, 24, &owed)
	tp.Draw(screen)
	tp.SetTaskID("different")
	tp.ResetVT()
	b, _ := os.ReadFile(logPath)
	testutil.Contains(t, string(b), "reason=rebind")
	testutil.False(t, tp.RerenderOwed())
}

// resize-without-restart (review): the owed state is evaluated on EVENTS only —
// bind, width change, explicit invalidation — never once per frame.
func TestTerminalPane_OwedEvaluatedOnEventsNotPerDraw(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	tp := NewTerminalPane()
	tp.SetTaskID("evt")
	calls := 0
	hints := 0
	tp.SetOwedFunc(func(string, agentview.TerminalAdapter, int) (bool, string) {
		calls++
		return true, ""
	}, func() string { hints++; return "ctrl+n" })
	sess := &mockAdapter{alive: true, ptyCols: 98, ptyRows: 22}
	tp.SetSession(sess)
	screen := tcell.NewSimulationScreen("UTF-8")
	testutil.NoError(t, screen.Init())
	defer screen.Fini()
	screen.SetSize(100, 24)
	tp.SetRect(0, 0, 100, 24)

	for range 300 {
		tp.Draw(screen)
	}
	testutil.Equal(t, calls, 1) // the bind event, once
	testutil.Equal(t, hints, 1)
	testutil.Contains(t, screenRowText(screen, 0, 100), "width changed · ctrl+n re-render")

	// Width change = event.
	screen.SetSize(120, 24)
	tp.SetRect(0, 0, 120, 24)
	for range 50 {
		tp.Draw(screen)
	}
	testutil.Equal(t, calls, 2)

	// Explicit invalidation = event.
	tp.InvalidateOwed()
	for range 50 {
		tp.Draw(screen)
	}
	testutil.Equal(t, calls, 3)

	// New session (re-render restart) = event.
	tp.SetSession(&mockAdapter{alive: true, ptyCols: 118, ptyRows: 22})
	for range 50 {
		tp.Draw(screen)
	}
	testutil.Equal(t, calls, 4)

	// Same session re-set every tick (the App does) is NOT an event.
	cur := tp.Session()
	for range 50 {
		tp.SetSession(cur)
		tp.Draw(screen)
	}
	testutil.Equal(t, calls, 4)
}
