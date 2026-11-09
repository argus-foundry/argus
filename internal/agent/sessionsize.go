package agent

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// SessionSizePath returns the path to the sidecar file recording a task
// session's last PTY size: ~/.argus/sessions/<taskID>.size.
//
// The PTY size is only available while the session is alive (SessionStatus
// RPC); once the process exits the dimensions are gone, but the on-disk
// session log keeps bytes formatted for that width. The sidecar persists
// the size so dead-session preview rendering can re-emulate the log tail
// at the width the bytes were actually formatted for — re-emulating at
// pane width scrambles output (absolute cursor positioning clamps at the
// right edge and autowraps). See gotchas/pty-terminal.md.
func SessionSizePath(taskID string) string {
	return filepath.Join(SessionsDir(), taskID+".size")
}

// SaveSessionSize records the PTY size for a task's session. Best-effort:
// errors are swallowed — the preview falls back to pane dimensions when no
// sidecar exists, matching pre-sidecar behavior.
func SaveSessionSize(taskID string, cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	path := SessionSizePath(taskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	os.WriteFile(path, fmt.Appendf(nil, "%d %d\n", cols, rows), 0o600) //nolint:errcheck // best-effort
}

// LoadSessionSize reads the persisted PTY size for a task's session.
// Returns ok=false when the sidecar is missing or malformed.
func LoadSessionSize(taskID string) (cols, rows int, ok bool) {
	data, err := os.ReadFile(SessionSizePath(taskID))
	if err != nil {
		return 0, 0, false
	}
	if _, err := fmt.Sscanf(string(data), "%d %d", &cols, &rows); err != nil {
		return 0, 0, false
	}
	if cols <= 0 || rows <= 0 {
		return 0, 0, false
	}
	return cols, rows, true
}

// SizeMark records that the PTY had Cols x Rows from log byte offset Off
// onward (offset in the SESSION LOG FILE's byte space, the one coordinate every
// replay reader shares — never the ring/handle total, which skews from it).
type SizeMark struct {
	Off        int64
	Cols, Rows int
}

// SessionSizesPath returns the append-only width index for a task's session
// log: ~/.argus/sessions/<taskID>.sizes, one "<logOffset> <cols> <rows>" line
// per PTY size change. It lives beside the log (truncated with it at every
// session start, so it covers exactly one process lifetime) and keeps markers
// out of the byte stream, which the ring/log merge arithmetic must not see.
func SessionSizesPath(taskID string) string {
	return filepath.Join(SessionsDir(), taskID+".sizes")
}

// ResetSessionSizes truncates the index and records the starting size at
// offset 0. Best-effort: replay falls back to a single width without it.
func ResetSessionSizes(taskID string, cols, rows int) {
	if cols <= 0 || rows <= 0 {
		return
	}
	path := SessionSizesPath(taskID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	os.WriteFile(path, fmt.Appendf(nil, "0 %d %d\n", cols, rows), 0o600) //nolint:errcheck // best-effort
}

// AppendSessionSizeMark appends one size change. Best-effort; a failed append
// only coarsens replay back toward the previous mark's width.
func AppendSessionSizeMark(taskID string, off int64, cols, rows int) {
	if cols <= 0 || rows <= 0 || off < 0 {
		return
	}
	f, err := os.OpenFile(SessionSizesPath(taskID), os.O_WRONLY|os.O_APPEND|os.O_CREATE, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	f.Write(fmt.Appendf(nil, "%d %d %d\n", off, cols, rows)) //nolint:errcheck // best-effort
}

// LoadSessionSizes reads the index. Malformed lines, a torn final line (no
// trailing newline) and offsets that go backwards are skipped, so a crash
// mid-append can never poison replay. Returns nil when there is no index.
func LoadSessionSizes(taskID string) []SizeMark {
	data, err := os.ReadFile(SessionSizesPath(taskID))
	if err != nil || len(data) == 0 {
		return nil
	}
	if i := bytes.LastIndexByte(data, '\n'); i < 0 {
		return nil
	} else {
		data = data[:i+1] // drop a torn trailing line
	}
	var marks []SizeMark
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		var m SizeMark
		if n, err := fmt.Sscanf(line, "%d %d %d", &m.Off, &m.Cols, &m.Rows); err != nil || n != 3 {
			continue
		}
		if m.Off < 0 || m.Cols <= 0 || m.Rows <= 0 {
			continue
		}
		if len(marks) > 0 && m.Off < marks[len(marks)-1].Off {
			continue
		}
		marks = append(marks, m)
	}
	return marks
}

// SizeMarkAt returns the mark in force at log offset off (the last mark whose
// Off <= off). ok=false when marks is empty or off precedes the first mark.
func SizeMarkAt(marks []SizeMark, off int64) (SizeMark, bool) {
	var found SizeMark
	ok := false
	for _, m := range marks {
		if m.Off > off {
			break
		}
		found, ok = m, true
	}
	return found, ok
}
