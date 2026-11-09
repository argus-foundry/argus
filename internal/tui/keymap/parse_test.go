package keymap

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/drn/argus/internal/testutil"
)

func TestParse_Valid(t *testing.T) {
	tests := []struct {
		spec string
		want Binding
	}{
		{"n", Binding{Key: tcell.KeyRune, Rune: 'n'}},
		{"J", Binding{Key: tcell.KeyRune, Rune: 'J'}},
		{"?", Binding{Key: tcell.KeyRune, Rune: '?'}},
		{"/", Binding{Key: tcell.KeyRune, Rune: '/'}},
		{"1", Binding{Key: tcell.KeyRune, Rune: '1'}},
		{"space", Binding{Key: tcell.KeyRune, Rune: ' '}},
		{"ctrl+l", Binding{Key: tcell.KeyCtrlL}},
		{"ctrl+D", Binding{Key: tcell.KeyCtrlD}}, // uppercase letter, lowercased
		{"control+r", Binding{Key: tcell.KeyCtrlR}},
		{"enter", Binding{Key: tcell.KeyEnter}},
		{"return", Binding{Key: tcell.KeyEnter}},
		{"esc", Binding{Key: tcell.KeyEscape}},
		{"escape", Binding{Key: tcell.KeyEscape}},
		{"tab", Binding{Key: tcell.KeyTab}},
		{"cmd+up", Binding{Key: tcell.KeyUp, Mods: tcell.ModCtrl | tcell.ModAlt}},
		{"opt+down", Binding{Key: tcell.KeyDown, Mods: tcell.ModCtrl | tcell.ModAlt}},
		{"alt+left", Binding{Key: tcell.KeyLeft, Mods: tcell.ModCtrl | tcell.ModAlt}},
		{"shift+up", Binding{Key: tcell.KeyUp, Mods: tcell.ModShift}},
		{"shift+pgdn", Binding{Key: tcell.KeyPgDn, Mods: tcell.ModShift}},
		{" ctrl+l ", Binding{Key: tcell.KeyCtrlL}}, // trimmed
	}
	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			got, err := Parse(tt.spec)
			testutil.NoError(t, err)
			testutil.Equal(t, got, tt.want)
		})
	}
}

func TestParse_Invalid(t *testing.T) {
	for _, spec := range []string{
		"", "  ",
		"ctrl+/",      // ctrl on non-letter
		"ctrl+left",   // no tcell ctrl-arrow key
		"ctrl+right",  // no tcell ctrl-arrow key
		"shift+j",     // modifier on a printable letter
		"cmd+x",       // modifier on a printable letter (non-arrow)
		"shift+enter", // modifier on a non-arrow named key
		"hyper+x",     // unknown modifier
		"nope",        // unknown multi-char base
	} {
		t.Run(spec, func(t *testing.T) {
			_, err := Parse(spec)
			testutil.Error(t, err)
		})
	}
}

func TestBinding_StringRoundTrip(t *testing.T) {
	// Every default binding must survive Parse(b.String()) == b so help display
	// and the parser stay in lock-step.
	for _, ctx := range AllContexts {
		for act, spec := range defaultSpecs[ctx] {
			b, err := Parse(spec)
			testutil.NoError(t, err)
			rt, err := Parse(b.String())
			if err != nil {
				t.Fatalf("%s/%s: String()=%q failed to re-parse: %v", ctx, act, b.String(), err)
			}
			testutil.Equal(t, rt, b)
		}
	}
}

func TestBinding_String(t *testing.T) {
	tests := []struct {
		b    Binding
		want string
	}{
		{Binding{Key: tcell.KeyRune, Rune: 'n'}, "n"},
		{Binding{Key: tcell.KeyRune, Rune: ' '}, "space"},
		{Binding{Key: tcell.KeyCtrlL}, "ctrl+l"},
		{Binding{Key: tcell.KeyEnter}, "enter"},
		{Binding{Key: tcell.KeyUp, Mods: tcell.ModCtrl | tcell.ModAlt}, "cmd+up"},
		{Binding{Key: tcell.KeyDown, Mods: tcell.ModShift}, "shift+down"},
	}
	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			testutil.Equal(t, tt.b.String(), tt.want)
		})
	}
}

// TestCtrlKeyConstants pins the assumption that the ctrlKeys map matches tcell's
// named constants, so a tcell bump that renumbers them is caught here.
func TestCtrlKeyConstants(t *testing.T) {
	testutil.Equal(t, ctrlKeys['l'], tcell.KeyCtrlL)
	testutil.Equal(t, ctrlKeys['d'], tcell.KeyCtrlD)
	testutil.Equal(t, ctrlKeys['z'], tcell.KeyCtrlZ)
	testutil.Equal(t, ctrlLetters[tcell.KeyCtrlL], 'l')
}

// TestParse_CtrlBackslash covers the resident toggle's default keyspec
// (add-resident-agent D9): 0x1c reaches the TUI in any terminal, and unlike the
// ctrl+<letter> family it is not in ctrlKeys.
func TestParse_CtrlBackslash(t *testing.T) {
	b, err := Parse(`ctrl+\`)
	testutil.NoError(t, err)
	testutil.Equal(t, b, Binding{Key: tcell.KeyCtrlBackslash})
	testutil.Equal(t, b.String(), `ctrl+\`)
	rt, err := Parse(b.String())
	testutil.NoError(t, err)
	testutil.Equal(t, rt, b)
	testutil.True(t, b.Matches(tcell.NewEventKey(tcell.KeyCtrlBackslash, 0, 0)))
	testutil.Equal(t, b.Matches(tcell.NewEventKey(tcell.KeyCtrlJ, 0, 0)), false)
	// Other ctrl+punctuation stays unsupported.
	for _, bad := range []string{"ctrl+/", "ctrl+]", "ctrl+1"} {
		_, err := Parse(bad)
		testutil.Error(t, err)
	}
}

// TestCtrlBackslashConstant pins that tcell's KeyCtrlBackslash is the key for
// control byte 0x1c (tcell offsets ctrl keys by 64) and matches the event
// tcell's input decoder posts for that byte (KeyCtrlSpace+0x1c with ModCtrl),
// so the Cmd+` terminal recipe really reaches Resolve.
func TestCtrlBackslashConstant(t *testing.T) {
	testutil.Equal(t, int(tcell.KeyCtrlBackslash), int(tcell.KeyCtrlSpace)+0x1c)
	decoded := tcell.NewEventKey(tcell.KeyCtrlSpace+tcell.Key(0x1c), 0, tcell.ModCtrl)
	testutil.Equal(t, decoded.Key(), tcell.KeyCtrlBackslash)
	b, err := Parse(`ctrl+\`)
	testutil.NoError(t, err)
	testutil.True(t, b.Matches(decoded))
}
