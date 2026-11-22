package keymap

import (
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/gdamore/tcell/v2"
)

func TestBoardTabKeys(t *testing.T) {
	keys := DefaultKeymap()
	for _, tc := range []struct {
		key tcell.Key
		act Action
	}{{tcell.KeyLeft, ActBoardTabPrev}, {tcell.KeyRight, ActBoardTabNext}} {
		t.Run(string(tc.act), func(t *testing.T) {
			act, ok := keys.Resolve(CtxHeraBoard, ev(tc.key, 0, 0))
			testutil.True(t, ok)
			testutil.Equal(t, act, tc.act)
			for _, mod := range []tcell.ModMask{tcell.ModShift, tcell.ModCtrl, tcell.ModAlt, tcell.ModCtrl | tcell.ModAlt} {
				_, ok = keys.Resolve(CtxHeraBoard, ev(tc.key, 0, mod))
				testutil.False(t, ok)
			}
		})
	}
	keys, warns := Build(config.Keybindings{HeraBoard: map[string]string{"tab_prev": "h", "tab_next": "l"}})
	testutil.Equal(t, len(warns), 0)
	act, ok := keys.Resolve(CtxHeraBoard, ev(tcell.KeyRune, 'l', 0))
	testutil.True(t, ok)
	testutil.Equal(t, act, ActBoardTabNext)
	_, ok = keys.Resolve(CtxHeraBoard, ev(tcell.KeyRight, 0, 0))
	testutil.False(t, ok)
}
