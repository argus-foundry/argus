package keymap

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestCtrlBackslashEncodings(t *testing.T) {
	t.Parallel()
	b, err := Parse(`ctrl+\`)
	testutil.NoError(t, err)
	km := DefaultKeymap()
	for _, tc := range []struct {
		name string
		ev   *tcell.EventKey
		want bool
	}{
		{"legacy", tcell.NewEventKey(tcell.KeyCtrlBackslash, 0, tcell.ModCtrl), true},
		{"legacy without modifier", tcell.NewEventKey(tcell.KeyCtrlBackslash, 0, 0), true},
		// CSI 92;5u and CSI 27;5;92~ both produce this event. Unlike
		// ctrl+letters, NewEventKey leaves ctrl+punctuation as KeyRune.
		{"extended", tcell.NewEventKey(tcell.KeyRune, '\\', tcell.ModCtrl), true},
		{"plain backslash", tcell.NewEventKey(tcell.KeyRune, '\\', 0), false},
		{"alt backslash", tcell.NewEventKey(tcell.KeyRune, '\\', tcell.ModAlt), false},
		{"other control punctuation", tcell.NewEventKey(tcell.KeyRune, ']', tcell.ModCtrl), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.Equal(t, b.Matches(tc.ev), tc.want)
			act, ok := km.Resolve(CtxGlobal, tc.ev)
			testutil.Equal(t, ok, tc.want)
			if tc.want {
				testutil.Equal(t, act, ActGlobalResident)
			}
		})
	}
	extended := tcell.NewEventKey(tcell.KeyRune, '\\', tcell.ModCtrl)
	testutil.Equal(t, extended.Key(), tcell.KeyRune)
	testutil.Equal(t, extended.Rune(), '\\')
	testutil.Equal(t, extended.Modifiers(), tcell.ModCtrl)
	// A printable binding must not steal the control chord, even when the
	// resident action is rebound away from its default.
	plain, err := Parse(`\`)
	testutil.NoError(t, err)
	testutil.False(t, plain.Matches(extended))
	rebound, warnings := Build(config.Keybindings{Global: map[string]string{"resident": "ctrl+x", "help": `\`}})
	testutil.Equal(t, len(warnings), 0)
	_, ok := rebound.Resolve(CtxGlobal, extended)
	testutil.False(t, ok)
}

func TestCtrlBackslashTerminalSequences(t *testing.T) {
	t.Parallel()
	b, err := Parse(`ctrl+\`)
	testutil.NoError(t, err)
	km := DefaultKeymap()
	for _, tc := range []struct {
		name, sequence string
		key            tcell.Key
		r              rune
	}{
		{"legacy", "\x1c", tcell.KeyCtrlBackslash, 0},
		{"CSI-u", "\x1b[92;5u", tcell.KeyRune, '\\'},
		{"modifyOtherKeys", "\x1b[27;5;92~", tcell.KeyRune, '\\'},
	} {
		t.Run(tc.name, func(t *testing.T) {
			events := make(chan tcell.Event, 8)
			tcell.NewInputProcessor(events).ScanUTF8([]byte(tc.sequence))
			select {
			case event := <-events:
				ev, ok := event.(*tcell.EventKey)
				testutil.True(t, ok)
				if !ok {
					return
				}
				testutil.Equal(t, ev.Key(), tc.key)
				testutil.Equal(t, ev.Rune(), tc.r)
				testutil.Equal(t, ev.Modifiers(), tcell.ModCtrl)
				testutil.True(t, b.Matches(ev))
				act, found := km.Resolve(CtxGlobal, ev)
				testutil.True(t, found)
				testutil.Equal(t, act, ActGlobalResident)
			default:
				t.Fatal("tcell did not decode a key event")
			}
		})
	}
}
