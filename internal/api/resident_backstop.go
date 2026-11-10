package api

import (
	"fmt"
	"log/slog"
	"strings"
	"unicode"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/hera"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// notifyResidentWorkerPrompts runs on the single detector goroutine. The
// episode is the union of detected prompts and self-reported blocked status;
// a transition between those signals must not send a second notice.
func (s *Server) notifyResidentWorkerPrompts(state *idleWatcherState, needs map[string]bool, tailOf func(string) []byte) {
	if s.db == nil {
		return
	}
	now := make(map[string]bool)
	for id, v := range needs {
		if v {
			now[id] = true
		}
	}
	blocked, err := s.db.ListBlockedHeraRoleBindings()
	if err != nil {
		slog.Warn("[resident] blocked-worker scan failed", "err", err)
		return
	}
	for _, b := range blocked {
		now[b.TaskID] = true
	}
	next := map[string]bool{}
	for id := range now {
		worker, err := s.db.IsResidentWorker(id)
		if err != nil {
			next[id] = state.residentPromptEpisodes[id]
			uxlog.Log("[resident] worker identity failed task=%s: %v", id, err)
			slog.Warn("[resident] worker identity failed", "task", id, "err", err)
			continue
		}
		if !worker {
			continue
		}
		task, err := s.db.Get(id)
		if err != nil {
			next[id] = state.residentPromptEpisodes[id]
			uxlog.Log("[resident] prompt worker lookup failed task=%s: %v", id, err)
			slog.Warn("[resident] prompt worker lookup failed", "task", id, "err", err)
			continue
		}
		if task == nil || task.Archived {
			continue
		}
		if state.residentPromptEpisodes[id] {
			next[id] = true
			continue
		}
		bindings, err := s.db.ListHeraLiveBindingsByTask(id)
		if err != nil {
			uxlog.Log("[resident] prompt bindings lookup failed task=%s: %v", id, err)
			slog.Warn("[resident] prompt bindings lookup failed", "task", id, "err", err)
			continue
		}
		for _, b := range bindings {
			standing, err := s.db.IsResidentOrchestrator(b.OrchestratorID)
			if err != nil {
				uxlog.Log("[resident] standing team lookup failed: %v", err)
				slog.Warn("[resident] standing team lookup failed", "err", err)
			}
			if err != nil || !standing {
				continue
			}
			role, err := s.db.HeraRole(b.RoleID)
			if err != nil {
				uxlog.Log("[resident] prompt role lookup failed: %v", err)
				slog.Warn("[resident] prompt role lookup failed", "err", err)
			}
			if err != nil || role.Kind != db.HeraKindWorker {
				continue
			}
			coords, err := s.db.ListHeraRolesByKind(b.OrchestratorID, db.HeraKindCoordinator)
			if err != nil || len(coords) != 1 {
				uxlog.Log("[resident] prompt coordinator lookup skipped count=%d err=%v", len(coords), err)
				slog.Warn("[resident] prompt coordinator lookup skipped", "count", len(coords), "err", err)
				continue
			}
			tail := []rune(strings.Map(func(r rune) rune {
				if unicode.IsControl(r) && r != '\n' {
					return -1
				}
				return r
			}, string(tailOf(id))))
			if len(tail) > 600 {
				tail = tail[len(tail)-600:]
			}
			body := fmt.Sprintf("Worker %s appears to be waiting on a prompt or reported blocked:\n%s\nOptions: reply with hera_send, nudge, or hera_delete_worker.", role.Name, string(tail))
			tldr := "Resident worker needs input" // fixed length, arbitrary role names stay in body
			var notifier hera.Notifier
			if s.notifier != nil {
				notifier = s.notifier
			}
			_, err = hera.New(s.db, notifier).Send(role.ID, coords[0].ID, body, tldr, nil)
			if err != nil {
				uxlog.Log("[resident] prompt notice failed task=%s: %v", id, err)
				slog.Warn("[resident] prompt notice failed", "task", id, "err", err)
				continue
			}
			next[id] = true
			uxlog.Log("[resident] worker prompt episode notified task=%s", id)
			slog.Info("[resident] worker prompt episode notified", "task", id)
			break
		}
	}
	state.residentPromptEpisodes = next
}
