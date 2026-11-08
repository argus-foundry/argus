package agentview

// ComposerTyped folds one chunk of genuine user input into a conservative
// "the agent's composer may hold unsent text" flag, for backends with no
// structured draft report (kick-timing-keep-place).
//
// Printable bytes and bracketed-paste content set it; Enter (CR), Ctrl+C and
// Ctrl+U clear it; escape sequences (arrows, SGR mouse, focus events, SS3),
// other control bytes and DEL are ignored — DEL cannot prove the box is empty
// again, so typed-then-erased stays flagged until the next submit. A false
// positive only delays a width re-render; a false negative would lose input.
func ComposerTyped(typed bool, p []byte) bool {
	pasting := false
	for i := 0; i < len(p); i++ {
		b := p[i]
		switch {
		case b == 0x1b:
			i = skipEscape(p, i, &pasting)
		case pasting:
			typed = true
		case b == '\r', b == 0x03, b == 0x15:
			typed = false
		case b < 0x20, b == 0x7f:
			// other control bytes: no effect
		default:
			typed = true
		}
	}
	return typed
}

// skipEscape consumes the escape sequence starting at p[i] and returns the
// index of its last byte. Bracketed-paste start/end toggle pasting.
func skipEscape(p []byte, i int, pasting *bool) int {
	if i+1 >= len(p) {
		return i
	}
	switch p[i+1] {
	case '[':
		j := i + 2
		for j < len(p) && p[j] >= 0x20 && p[j] <= 0x3f {
			j++
		}
		if j >= len(p) {
			return len(p) - 1
		}
		switch string(p[i : j+1]) {
		case "\x1b[200~":
			*pasting = true
		case "\x1b[201~":
			*pasting = false
		}
		return j
	case 'O':
		if i+2 < len(p) {
			return i + 2
		}
		return i + 1
	default:
		return i + 1 // Alt+key: ESC plus one byte
	}
}
