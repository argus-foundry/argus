package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

func (s *Server) decisionNavigation() ([]heramodel.BoardCoordinator, error) {
	m, err := heramodel.BuildModel(s.db, nil, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	rows := m.BoardNavigation()
	counts, err := s.db.HeraBoardOpenCounts()
	if err != nil {
		return nil, err
	}
	for n := range rows {
		rows[n].Deferred = counts[rows[n].RoleID].Deferred
		rows[n].Decisions = counts[rows[n].RoleID].Decisions
		rows[n].FYIs = counts[rows[n].RoleID].FYIs
		rows[n].Actions = counts[rows[n].RoleID].Actions
		rows[n].Todos = counts[rows[n].RoleID].Todos
		rows[n].ReadyActions = counts[rows[n].RoleID].ReadyActions
	}
	return rows, nil
}

func (s *Server) decisionVisible(role int64) (bool, error) {
	rows, err := s.decisionNavigation()
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		if row.RoleID == role {
			return true, nil
		}
	}
	return false, nil
}

func decisionReadAccess(r *http.Request) bool {
	origin := authOrigin(r)
	return origin == "master" || origin == "device" || origin == "rail-board"
}

func (s *Server) handleDecisionNavigation(w http.ResponseWriter, r *http.Request) {
	if !decisionReadAccess(r) {
		writeErr(w, 403, "board access required", nil)
		return
	}
	rows, err := s.decisionNavigation()
	if err != nil {
		uxlog.Log("[decide] navigation failed")
		writeErr(w, 500, "boards unavailable", nil)
		return
	}
	uxlog.Log("[decide] navigation count=%d", len(rows))
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"roles": rows})
}

func (s *Server) handleDecisionCounts(w http.ResponseWriter, r *http.Request) {
	if origin := authOrigin(r); origin != "master" && origin != "device" {
		writeErr(w, 403, "master or device token required", nil)
		return
	}
	rows, err := s.decisionNavigation()
	if err != nil {
		uxlog.Log("[decide] counts failed")
		writeErr(w, 500, "boards unavailable", nil)
		return
	}
	decisions, fyis, actions, todos := 0, 0, 0, 0
	readyActions := 0
	for _, row := range rows {
		decisions += row.Decisions
		fyis += row.FYIs
		actions += row.Actions
		todos += row.Todos
		readyActions += row.ReadyActions
	}
	uxlog.Log("[decide] counts roles=%d decisions=%d fyis=%d", len(rows), decisions, fyis)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"roles": rows, "decisions": decisions, "fyis": fyis, "actions": actions, "todos": todos, "ready_actions": readyActions, "total": decisions + fyis + readyActions})
}

// decisionSessionRoute is deliberately an exact method/path allowlist. Internal
// tags are overwritten after credential verification, never trusted from HTTP.
func decisionSessionRoute(method, path string) bool {
	if method == http.MethodGet && path == "/api/hera/board/decision-navigation" {
		return true
	}
	parts := strings.Split(path, "/")
	if len(parts) == 8 && parts[1] == "api" && parts[2] == "hera" && parts[3] == "roles" && parts[5] == "board" && parts[7] == "defer" && method == http.MethodPut {
		role, e := strconv.ParseInt(parts[4], 10, 64)
		item, e2 := strconv.ParseInt(parts[6], 10, 64)
		return e == nil && e2 == nil && role > 0 && item > 0 && fmt.Sprint(role) == parts[4] && fmt.Sprint(item) == parts[6]
	}
	if len(parts) != 7 || parts[1] != "api" || parts[2] != "hera" || parts[3] != "roles" || parts[5] != "board" {
		return false
	}
	role, err := strconv.ParseInt(parts[4], 10, 64)
	if err != nil || role <= 0 || fmt.Sprint(role) != parts[4] {
		return false
	}
	return method == http.MethodGet && (parts[6] == "decisions" || parts[6] == "decide-items") || method == http.MethodPost && (parts[6] == "answers" || parts[6] == "fyi-actions")
}

func (s *Server) handleDecideItems(w http.ResponseWriter, r *http.Request) {
	role, ok := s.decisionRole(w, r)
	if !ok {
		return
	}
	items, err := s.db.ListHeraBoardItems(role, false)
	if err != nil {
		uxlog.Log("[decide] mixed read failed role=%d", role)
		writeErr(w, 500, "board unavailable", nil)
		return
	}
	coord, err := s.db.HeraRole(role)
	if err != nil {
		writeErr(w, 500, "board unavailable", nil)
		return
	}
	orch, err := s.db.HeraOrchestrator(coord.OrchestratorID)
	if err != nil {
		writeErr(w, 500, "board unavailable", nil)
		return
	}
	decisions, fyis, actions, todos := 0, 0, 0, 0
	deferred := 0
	visible := items[:0]
	for _, item := range items {
		if item.DeferredAt != nil {
			deferred++
			if r.URL.Query().Get("include_deferred") != "true" {
				continue
			}
		}
		visible = append(visible, item)
	}
	items = visible
	for _, item := range items {
		switch item.Kind {
		case "decision":
			decisions++
		case "fyi":
			fyis++
		case "action":
			actions++
		case "todo":
			todos++
		}
	}
	uxlog.Log("[decide] mixed read role=%d decisions=%d fyis=%d", role, decisions, fyis)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"role_id": role, "orchestrator": orch.Name, "coordinator": coord.Name, "decisions": decisions, "fyis": fyis, "actions": actions, "todos": todos, "deferred_count": deferred, "items": items})
}
