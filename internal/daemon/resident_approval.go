package daemon

import (
	"errors"
	"path/filepath"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
)

type ResidentPermissionReq struct {
	Token string
	Call  db.ApprovalCall
}

type ResidentPermissionResp struct {
	Allow   bool
	BoardID int64
	State   string
	Error   string
}

// ResidentPermission is a private-file authenticated hook endpoint, never a
// human decision endpoint. It can queue or consume a grant, not create one.
func (s *RPCService) ResidentPermission(req *ResidentPermissionReq, resp *ResidentPermissionResp) error {
	launch, err := agent.ReadResidentApprovalLaunch()
	if err == nil {
		err = validateResidentPermission(s.daemon.db, launch, req)
	}
	if err != nil {
		resp.Error = "resident permission hook authentication failed"
		residentLog("[resident-approval] hook refused: %v", err)
		return nil
	}
	// The permission chooser can render before the command hook's RPC. The
	// detector also debounces permissions, closing that pre-RPC window.
	if srv := s.daemon.apiServer; srv != nil {
		srv.ResidentPermissionAnswered(req.Call.TaskID, req.Call.Tool, req.Call.Input)
	}
	a, allow, err := s.daemon.db.RequestResidentApproval(req.Call, time.Now())
	if err != nil {
		resp.Error = "resident permission request could not be queued"
		residentLog("[resident-approval] hook failed: %v", err)
		return nil
	}
	resp.Allow, resp.BoardID, resp.State = allow, a.BoardID, a.State
	residentLog("[resident-approval] hook answered item=%d allow=%t state=%s", a.BoardID, allow, a.State)
	return nil
}

func validateResidentPermission(database *db.DB, launch agent.ResidentApprovalLaunch, req *ResidentPermissionReq) error {
	c := req.Call
	if !launch.Matches(req.Token, c.Launch) || c.TaskID != launch.TaskID || c.Session == "" || (launch.Session != "" && c.Session != launch.Session) || cleanApprovalCwd(c.Cwd) != cleanApprovalCwd(launch.Cwd) {
		return errors.New("launch identity mismatch")
	}
	r, err := database.ResidentTask()
	if err != nil {
		return err
	}
	if r == nil || r.ID != c.TaskID || r.Account != launch.Account || (r.SessionID != "" && r.SessionID != c.Session) || cleanApprovalCwd(r.Worktree) != cleanApprovalCwd(c.Cwd) {
		return errors.New("resident identity mismatch")
	}
	return nil
}

func cleanApprovalCwd(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	return filepath.Clean(path)
}
