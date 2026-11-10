package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/hera"
	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/mergesafety"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// Shared with graduation: target membership must stay live from validation
// through the destructive mutation. Never hold this during a restart wait.
var residentWorkerMutationMu sync.Mutex

var residentDeleteTool = Tool{Name: "hera_delete_worker", Description: "Resident only: delete a live worker of your standing team. Refuses unmerged non-empty work, and any uncommitted or untracked files, unless force=true. Force returns the unmerged summary; REPORT it to Aaron in your own conversation in this turn.", InputSchema: map[string]interface{}{
	"type": "object", "properties": map[string]interface{}{"role": map[string]interface{}{"type": "string", "description": "Worker name or numeric role id"}, "force": map[string]interface{}{"type": "boolean"}}, "required": []string{"role"},
}}

func (s *Server) boundResident(caller string) bool {
	if caller == "" || s.taskDB == nil {
		return false
	}
	t, err := s.taskDB.Get(caller)
	return err == nil && t != nil && t.IsResident()
}

// residentPromotionGuard runs before the coordinator scan and fails closed on
// unreadable membership or marker state. The marker also covers pre-launch.
func (s *Server) residentPromotionGuard(t *model.Task) error {
	if t == nil {
		return fmt.Errorf("cannot classify resident caller")
	}
	if t.IsResident() {
		return fmt.Errorf("the resident already owns its standing orchestrator; use hera_spawn_worker with an explicit project instead")
	}
	if d, ok := s.heraStore.(interface{ IsResidentWorker(string) (bool, error) }); ok {
		yes, err := d.IsResidentWorker(t.ID)
		if err != nil {
			return fmt.Errorf("resident worker check failed: %w", err)
		}
		if yes {
			return fmt.Errorf("you are a resident worker; ask the resident to graduate you (hera_send), do not create an orchestrator yourself")
		}
	} else {
		return fmt.Errorf("cannot classify resident worker membership")
	}
	return nil
}

func (s *Server) toolHeraNewOrchestratorAs(id interface{}, args json.RawMessage, caller string) *Response {
	if !s.heraEnabled() {
		return residentToolError(id, "hera not configured")
	}
	if caller != "" {
		t, err := s.taskDB.Get(caller)
		if err != nil {
			return residentToolError(id, "cannot verify bound caller")
		}
		if err = s.residentPromotionGuard(t); err != nil {
			return residentToolError(id, err.Error())
		}
		var p map[string]interface{}
		if err = json.Unmarshal(args, &p); err != nil {
			return residentToolError(id, err.Error())
		}
		if p == nil {
			return residentToolError(id, "invalid Hera arguments")
		}
		p["cwd"] = t.Worktree
		args, _ = json.Marshal(p)
	}
	return s.toolHeraNewOrchestrator(id, args)
}

func (s *Server) toolHeraDeleteWorker(id interface{}, args json.RawMessage, caller string) *Response {
	// Identity comes only from the transport's caller binding. Ignore cwd/id.
	if !s.boundResident(caller) {
		return residentToolError(id, "hera_delete_worker is resident only (connection-bound caller required)")
	}
	residentWorkerMutationMu.Lock()
	defer residentWorkerMutationMu.Unlock()
	d, ok := s.taskDB.(*db.DB)
	if !ok || !s.heraEnabled() {
		return residentToolError(id, "resident delete not configured")
	}
	runner, ok := s.taskStopper.(hera.NukeRunner)
	if !ok {
		return residentToolError(id, "resident delete runner not configured")
	}
	var p struct {
		Role  string `json:"role"`
		Force bool   `json:"force"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return residentToolError(id, err.Error())
	}
	orch, err := d.HeraOrchestratorByName(db.ResidentOrchestratorName)
	if err != nil {
		return residentToolError(id, err.Error())
	}
	own, err := d.HeraLiveBindingByTaskAndOrchestrator(caller, orch.ID)
	if err != nil {
		return residentToolError(id, "resident standing binding unavailable")
	}
	role, err := d.HeraRoleByName(orch.ID, p.Role)
	if err != nil {
		if rid, e := strconv.ParseInt(p.Role, 10, 64); e == nil {
			role, err = d.HeraRole(rid)
		}
	}
	if err != nil || role == nil || role.OrchestratorID != orch.ID {
		return residentToolError(id, "target is not a live worker of the resident orchestrator")
	}
	if role.ID == own.RoleID {
		return residentToolError(id, "resident delete must never target itself")
	}
	if role.Kind != db.HeraKindWorker || role.NukedAt != nil {
		return residentToolError(id, "target is not a live worker of the resident orchestrator")
	}
	b, err := d.HeraLiveBindingByRole(role.ID)
	if err != nil {
		return residentToolError(id, "target is not a live worker of the resident orchestrator (possibly graduated)")
	}
	task, err := d.Get(b.ArgusTaskID)
	if err != nil {
		return residentToolError(id, err.Error())
	}
	if task.IsResident() || task.ID == caller {
		return residentToolError(id, "resident delete must never target itself")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cfg := d.Config()
	pr := ""
	entries, metaErr := d.ListMeta(task.ID, "pr")
	if metaErr == nil {
		for _, e := range entries {
			if e.Key == "state" {
				pr = e.Value
			}
		}
	} else {
		pr = fmt.Sprintf("unknown (%v)", metaErr)
	}
	summary := mergesafety.SummarizeTask(ctx, cfg, task, pr)
	if !p.Force {
		classify := s.residentClassify
		if classify == nil {
			classify = func(ctx context.Context, t *model.Task) (mergesafety.Verdict, error) {
				return mergesafety.ClassifyTask(ctx, cfg, t)
			}
		}
		v, err := classify(ctx, task)
		if err != nil {
			return residentToolError(id, fmt.Sprintf("not safe: classifier failed: %v", err))
		}
		// A safe verdict speaks only for the branch: uncommitted or untracked
		// files would be destroyed with the worktree. Unknown counts as dirty.
		if summary.DirtyCount != "0" {
			return residentToolError(id, fmt.Sprintf("not safe: worktree has uncommitted or untracked changes (dirty files: %s); commit them, or pass force=true", summary.DirtyCount))
		}
		if !v.Safe && !summary.Empty() {
			return residentToolError(id, fmt.Sprintf("not safe: %s; commits ahead: %s; dirty files: %s", v.Reason, summary.CommitsAhead, summary.DirtyCount))
		}
	}
	if p.Force {
		// The tool result is the summary's only delivery to the resident, and the
		// nuke below can outlast a client-side MCP timeout. Persist it first.
		raw, _ := json.Marshal(summary)
		uxlog.Log("[resident] force delete worker=%s task=%s summary=%s", role.Name, task.ID, raw)
		slog.Warn("[resident] force delete", "worker", role.Name, "task", task.ID, "summary", string(raw))
	}
	// Synchronous closure execution ensures the result follows the reclaim work.
	// The shared primitive preserves multi-bound tasks and rechecks reclaim safety.
	hera.NukeSingleRoleAs(d, runner, &heramodel.RoleView{RoleID: role.ID, TaskID: task.ID, Live: true}, db.HeraActorResident)()
	updated, err := d.HeraRole(role.ID)
	if err != nil || updated.NukedAt == nil {
		return residentToolError(id, "worker nuke did not complete")
	}
	uxlog.Log("[resident] deleted worker=%s task=%s force=%t", role.Name, task.ID, p.Force)
	slog.Info("[resident] deleted worker", "worker", role.Name, "task", task.ID, "force", p.Force)
	var out strings.Builder
	fmt.Fprintf(&out, "Deleted worker %s.\n", role.Name)
	if p.Force {
		raw, _ := json.MarshalIndent(summary, "", "  ")
		fmt.Fprintf(&out, "\nUnmerged summary (REPORT to Aaron now):\n%s\n", raw)
	}
	return toolResult(id, out.String())
}

func residentToolError(id interface{}, message string) *Response {
	uxlog.Log("[resident] tool refused: %s", message)
	slog.Warn("[resident] tool refused", "reason", message)
	return toolError(id, message)
}

// verifiedCaller returns the connection-bound caller only when it is the
// resident presenting the current launch's token; every other connection,
// including the resident's id with a missing, wrong or superseded token, is
// unbound (""). The token is never logged.
func (s *Server) verifiedCaller(r *http.Request) string {
	caller := r.URL.Query().Get("caller")
	if caller == "" {
		return ""
	}
	token := r.Header.Get(agent.ResidentMCPTokenHeader)
	hashPath := s.residentTokenHashPath
	if hashPath == nil {
		hashPath = agent.ResidentMCPTokenHashPath
	}
	path, err := hashPath()
	if err == nil && s.boundResident(caller) && agent.VerifyResidentMCPToken(path, token) {
		return caller
	}
	uxlog.Log("[resident] unverified caller refused: caller=%q token_present=%t", caller, token != "")
	slog.Warn("[resident] unverified caller refused", "caller", caller, "token_present", token != "") //nolint:gosec // G706: slog encodes attributes, escaping control characters; the token itself is never logged
	return ""
}
