package agentview

import "testing"

func TestComposerTyped(t *testing.T) {
	tests := []struct {
		name  string
		start bool
		in    string
		want  bool
	}{
		{"printable sets", false, "a", true},
		{"utf8 sets", false, "é", true},
		{"enter clears", true, "\r", false},
		{"text then enter clears", false, "hello\r", false},
		{"enter then text sets", false, "\rhello", true},
		{"ctrl+c clears", true, "\x03", false},
		{"ctrl+u clears", true, "\x15", false},
		{"backspace does not unset", true, "\x7f", true},
		{"backspace does not set", false, "\x7f", false},
		{"arrow ignored when clean", false, "\x1b[A", false},
		{"arrow ignored when dirty", true, "\x1b[B", true},
		{"ss3 ignored", false, "\x1bOA", false},
		{"sgr mouse ignored", false, "\x1b[<0;10;5M", false},
		{"focus event ignored", false, "\x1b[I", false},
		{"lone escape ignored", false, "\x1b", false},
		{"alt+enter ignored", false, "\x1b\r", false},
		{"bracketed paste sets", false, "\x1b[200~pasted\x1b[201~", true},
		{"paste newline sets", false, "\x1b[200~\n\x1b[201~", true},
		{"empty paste leaves clean", false, "\x1b[200~\x1b[201~", false},
		{"paste then enter clears", false, "\x1b[200~x\x1b[201~\r", false},
		{"typing after arrow sets", false, "\x1b[Ax", true},
		{"empty input keeps state", true, "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ComposerTyped(tc.start, []byte(tc.in)); got != tc.want {
				t.Fatalf("ComposerTyped(%v, %q) = %v, want %v", tc.start, tc.in, got, tc.want)
			}
		})
	}
}
