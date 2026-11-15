package agent

import (
	"log/slog"
	"strings"

	"github.com/argus-foundry/argus/internal/sanitize"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// residentAutoFallback observes Claude's own safe auto->default downgrade.
// It never controls the process or retries work. Owned only by the PTY readLoop.
type residentAutoFallback struct {
	enabled, logged bool
	tail            string
}

func (w *residentAutoFallback) observe(taskID string, data []byte) {
	if !w.enabled || w.logged {
		return
	}
	w.tail += string(data)
	// Scan before trimming so a notice split across two full-sized PTY reads
	// is not lost at the retention boundary. readLoop reads at most 4096 bytes.
	text := strings.ToLower(strings.Join(strings.Fields(sanitize.StripANSI(w.tail)), " "))
	for _, reason := range []string{
		"auto mode disabled by settings",
		"auto mode is unavailable for your plan",
		"auto mode unavailable for this model",
		"auto mode unavailable while fast mode is on",
		"auto mode is unavailable right now",
	} {
		if strings.Contains(text, reason) {
			w.logged = true
			w.tail = ""
			uxlog.Log("[resident] auto permission fallback task=%s mode=default reason=%s", taskID, reason)
			slog.Info("[resident] auto permission fallback", "task", taskID, "mode", "default", "reason", reason)
			return
		}
	}
	if len(w.tail) > 4096 {
		w.tail = w.tail[len(w.tail)-4096:]
	}
}
