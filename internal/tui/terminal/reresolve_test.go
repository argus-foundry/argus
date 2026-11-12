package terminal

import (
	"testing"

	"github.com/argus-foundry/argus/internal/app/agentview"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestReResolveSession(t *testing.T) {
	dead := &mockAdapter{alive: false}
	live := &mockAdapter{alive: true}
	pick := func(s agentview.TerminalAdapter) func(string) agentview.TerminalAdapter {
		return func(string) agentview.TerminalAdapter { return s }
	}
	t.Run("empty task or nil resolver is a no-op", func(t *testing.T) {
		tp := NewTerminalPane()
		testutil.Equal(t, tp.ReResolveSession("", pick(live)), ReResolveNone)
		testutil.Equal(t, tp.ReResolveSession("t", nil), ReResolveNone)
	})
	t.Run("late bind attaches a session", func(t *testing.T) {
		tp := NewTerminalPane()
		testutil.Equal(t, tp.ReResolveSession("t", pick(live)), ReResolveLateBound)
		testutil.Equal(t, tp.Session() == agentview.TerminalAdapter(live), true)
	})
	t.Run("live bound handle is left alone", func(t *testing.T) {
		tp := NewTerminalPane()
		tp.SetSession(live)
		called := false
		out := tp.ReResolveSession("t", func(string) agentview.TerminalAdapter { called = true; return &mockAdapter{alive: true} })
		testutil.Equal(t, out, ReResolveNone)
		testutil.Equal(t, called, false)
	})
	t.Run("dead handle replaced by a live different one", func(t *testing.T) {
		tp := NewTerminalPane()
		tp.SetSession(dead)
		testutil.Equal(t, tp.ReResolveSession("t", pick(live)), ReResolveReplaced)
		testutil.Equal(t, tp.Session() == agentview.TerminalAdapter(live), true)
	})
	t.Run("dead handle kept when nothing, the same, or another dead one is offered", func(t *testing.T) {
		tp := NewTerminalPane()
		tp.SetSession(dead)
		testutil.Equal(t, tp.ReResolveSession("t", pick(nil)), ReResolveNone)
		testutil.Equal(t, tp.ReResolveSession("t", pick(dead)), ReResolveNone)
		testutil.Equal(t, tp.ReResolveSession("t", pick(&mockAdapter{alive: false})), ReResolveNone)
		testutil.Equal(t, tp.Session() == agentview.TerminalAdapter(dead), true)
	})
}
