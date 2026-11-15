package agent

import (
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// Notice text is a best-effort diagnostic, not the mechanism for safe fallback.
// Match eligibility/disablement cores rather than exact plan/model wording,
// without confusing the terminal missing-verdict stop with a Manual fallback.
var residentAutoNotice = regexp.MustCompile(`auto mode (?:disabled|(?:is )?(?:unavailable|not available) (?:for|while|right now))`)

// residentAutoFallback is owned only by readLoop. It observes startup output
// for at most 64 KiB or 60s from launch, then releases all parser/text storage.
type residentAutoFallback struct {
	enabled, logged, stopped bool
	started                  time.Time
	seen                     int
	parser                   *ansi.Parser
	chunk                    strings.Builder
	tail                     string // complete printable UTF-8 only; escape state lives in parser
}

func (w *residentAutoFallback) observe(taskID string, data []byte) {
	if !w.enabled || w.logged || w.stopped {
		return
	}
	w.observeAt(taskID, data, time.Now())
}

func (w *residentAutoFallback) observeAt(taskID string, data []byte, now time.Time) {
	if !w.enabled || w.logged || w.stopped {
		return
	}
	if w.started.IsZero() {
		w.started = now
	}
	if now.Sub(w.started) >= time.Minute {
		w.stop()
		return
	}
	if w.parser == nil {
		// Only text and CSI final bytes matter. Bound ignored string payloads
		// and parameter storage; the parser preserves fragmented UTF-8/ANSI.
		w.parser = new(ansi.Parser)
		w.parser.SetParamsSize(4)
		w.parser.SetDataSize(1)
		w.parser.SetHandler(ansi.Handler{
			Print: func(r rune) { w.chunk.WriteRune(r) },
			Execute: func(b byte) {
				if b == '\n' || b == '\r' || b == '\t' {
					w.chunk.WriteByte(' ')
				}
			},
			HandleCsi: func(cmd ansi.Cmd, _ ansi.Params) {
				if cmd.Final() != 'm' {
					w.chunk.WriteByte(' ')
				}
			},
		})
	}
	n := min(len(data), 64*1024-w.seen)
	w.seen += n
	w.parser.Parse(data[:n])
	w.tail += w.chunk.String()
	w.chunk.Reset()
	// Scan before trimming so a notice crossing full PTY reads is retained.
	text := strings.ToLower(strings.Join(strings.Fields(w.tail), " "))
	if reason := residentAutoNotice.FindString(text); reason != "" {
		w.logged = true
		// The supervisor does not initialize uxlog; slog is its real sink.
		slog.Info("[resident] auto permission fallback", "task", taskID, "mode", "default", "reason", reason)
		w.stop()
		return
	}
	if w.seen == 64*1024 {
		w.stop()
		return
	}
	if len(w.tail) > 4096 {
		cut := len(w.tail) - 4096
		for !utf8.RuneStart(w.tail[cut]) {
			cut++
		}
		w.tail = w.tail[cut:]
	}
}

func (w *residentAutoFallback) stop() {
	w.stopped = true
	w.tail = ""
	w.chunk.Reset()
	w.parser = nil
}
