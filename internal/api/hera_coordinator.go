package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/gitutil"
	"github.com/drn/argus/internal/hera/tiering"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/profiles"
	"github.com/drn/argus/internal/uxlog"
)

const (
	heraCoordMaxBody   = 64 << 10
	heraCoordMaxName   = 64
	heraCoordMaxPrompt = 64 << 10
)

var heraRequestIDRe = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type heraCoordDefaults struct {
	BaseBranch string `json:"base_branch"`
	Profile    string `json:"profile"`
	Archetype  string `json:"archetype"`
}

type heraCoordOptions struct {
	Branches   []string          `json:"branches"`
	Profiles   []string          `json:"profiles"`
	Archetypes []string          `json:"archetypes"`
	Defaults   heraCoordDefaults `json:"defaults"`
}

type heraCoordCreateReq struct {
	RequestID  string `json:"request_id"`
	Project    string `json:"project"`
	Name       string `json:"name"`
	Prompt     string `json:"prompt"`
	BaseBranch string `json:"base_branch"`
	Profile    string `json:"profile"`
	Archetype  string `json:"archetype"`
	Backend    string `json:"backend"`
	Model      string `json:"model"`
}

type heraCoordCreateResp struct {
	OrchestratorID int64  `json:"orchestrator_id"`
	RoleID         int64  `json:"role_id"`
	BindingID      int64  `json:"binding_id"`
	TaskID         string `json:"task_id"`
	Name           string `json:"name"`
	BaseBranch     string `json:"base_branch"`
	Profile        string `json:"profile"`
	Archetype      string `json:"archetype"`
}

type heraCoordCreatedEvent struct {
	OrchestratorID int64  `json:"orchestrator_id"`
	RoleID         int64  `json:"role_id"`
	TaskID         string `json:"task_id"`
}

// heraCoordProfileLoader is the loader a project's coordinator sees: the shared
// library plus the project checkout's in-repo profiles.
func heraCoordProfileLoader(p config.Project) *profiles.Loader {
	l := &profiles.Loader{LibraryDir: filepath.Join(db.DataDir(), "profiles")}
	if p.Path != "" {
		l.RepoDir = filepath.Join(p.Path, ".argus", "profiles")
	}
	return l
}

func heraCoordValidProfiles(cfg config.Config, p config.Project) []string {
	l := heraCoordProfileLoader(p)
	out := []string{}
	for _, name := range l.Discover() {
		if prof, errs := tiering.DefaultValidate(l, name, cfg); prof != nil && len(errs) == 0 {
			out = append(out, name)
		}
	}
	return out
}

func heraCoordDefaultBranch(p config.Project) string {
	if _, ref, err := gitutil.ResolveDefaultBranch(p.Path, p.Branch); err == nil {
		return ref
	}
	return p.Branch
}

func (s *Server) handleHeraCoordinatorOptions(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("project")
	cfg := s.db.Config()
	proj, ok := cfg.Projects[name]
	if name == "" || !ok {
		writeErr(w, http.StatusNotFound, "project not found", nil)
		return
	}
	defBranch := heraCoordDefaultBranch(proj)
	branches := gitutil.ListRemoteBranches(proj.Path)
	if branches == nil {
		branches = []string{}
	}
	if defBranch != "" {
		found := false
		for _, b := range branches {
			if b == defBranch {
				found = true
				break
			}
		}
		if !found {
			branches = append([]string{defBranch}, branches...)
		}
	}
	writeJSON(w, http.StatusOK, heraCoordOptions{
		Branches:   branches,
		Profiles:   heraCoordValidProfiles(cfg, proj),
		Archetypes: append([]string{}, profiles.CanonicalArchetypes...),
		Defaults: heraCoordDefaults{
			BaseBranch: defBranch,
			Profile:    proj.ResolveProfileName(),
			Archetype:  agent.DefaultCoordinatorArchetype,
		},
	})
}

func heraCoordNormalize(req *heraCoordCreateReq) {
	req.RequestID = strings.ToLower(strings.TrimSpace(req.RequestID))
	req.Project = strings.TrimSpace(req.Project)
	req.Name = strings.TrimSpace(req.Name)
	req.Prompt = strings.TrimSpace(req.Prompt)
	req.BaseBranch = strings.TrimSpace(req.BaseBranch)
	req.Profile = strings.TrimSpace(req.Profile)
	req.Archetype = strings.TrimSpace(req.Archetype)
	req.Backend = strings.TrimSpace(req.Backend)
	req.Model = strings.TrimSpace(req.Model)
}

// heraCoordBodyHash canonicalizes everything but request_id so a retry with the
// same intent hashes identically regardless of JSON key order or whitespace.
func heraCoordBodyHash(req heraCoordCreateReq) string {
	req.RequestID = ""
	b, _ := json.Marshal(req)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func heraCoordValidate(s *Server, cfg config.Config, req heraCoordCreateReq) (config.Project, int, string) {
	if !heraRequestIDRe.MatchString(req.RequestID) {
		return config.Project{}, http.StatusBadRequest, "request_id must be a UUID"
	}
	if req.Project == "" {
		return config.Project{}, http.StatusBadRequest, "project is required"
	}
	if req.Prompt == "" {
		return config.Project{}, http.StatusBadRequest, "prompt is required"
	}
	if len(req.Prompt) > heraCoordMaxPrompt {
		return config.Project{}, http.StatusBadRequest, "prompt too long"
	}
	if len(req.Name) > heraCoordMaxName || strings.ContainsFunc(req.Name, unicode.IsControl) {
		return config.Project{}, http.StatusBadRequest, "invalid name"
	}
	if strings.ContainsFunc(req.Model, unicode.IsControl) {
		return config.Project{}, http.StatusBadRequest, "invalid model"
	}
	proj, ok := cfg.Projects[req.Project]
	if !ok {
		return config.Project{}, http.StatusNotFound, "project not found"
	}
	if req.Archetype != "" {
		valid := false
		for _, a := range profiles.CanonicalArchetypes {
			if a == req.Archetype {
				valid = true
				break
			}
		}
		if !valid {
			return config.Project{}, http.StatusBadRequest, "unknown archetype " + req.Archetype
		}
	}
	if req.Profile != "" {
		found := false
		for _, p := range heraCoordValidProfiles(cfg, proj) {
			if p == req.Profile {
				found = true
				break
			}
		}
		if !found {
			return config.Project{}, http.StatusBadRequest, "unknown or invalid profile " + req.Profile
		}
	}
	if req.BaseBranch != "" {
		if err := gitutil.ValidateBranchRef(proj.Path, req.BaseBranch); err != nil {
			return config.Project{}, http.StatusBadRequest, err.Error()
		}
	}
	if err := s.validateBackend(req.Backend); err != nil {
		return config.Project{}, http.StatusBadRequest, err.Error()
	}
	return proj, 0, ""
}

func (s *Server) handleHeraCoordinatorCreate(w http.ResponseWriter, r *http.Request) {
	var req heraCoordCreateReq
	r.Body = http.MaxBytesReader(w, r.Body, heraCoordMaxBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON", err)
		return
	}
	heraCoordNormalize(&req)
	cfg := s.db.Config()
	proj, status, msg := heraCoordValidate(s, cfg, req)
	if status != 0 {
		writeErr(w, status, msg, nil)
		return
	}

	outcome, stored, err := s.db.BeginHeraCoordinatorRequest(req.RequestID, heraCoordBodyHash(req))
	if err != nil {
		uxlog.Log("[api-hera] create coordinator: receipt claim failed request=%s: %v", req.RequestID, err)
		writeErr(w, http.StatusInternalServerError, "failed to record request", err)
		return
	}
	switch outcome {
	case db.HeraCoordRequestReplay:
		uxlog.Log("[api-hera] create coordinator: replay request=%s", req.RequestID)
		w.Header().Set("Idempotent-Replay", "true")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(stored))
		return
	case db.HeraCoordRequestInFlight:
		uxlog.Log("[api-hera] create coordinator: in-flight retry request=%s", req.RequestID)
		writeErr(w, http.StatusConflict, "request already in progress", nil)
		return
	case db.HeraCoordRequestConflict:
		uxlog.Log("[api-hera] create coordinator: request_id reused with different body request=%s", req.RequestID)
		writeErr(w, http.StatusConflict, "request_id already used with a different body", nil)
		return
	}

	base := req.Name
	if base == "" {
		base = agent.DeriveHeraWorkerName(req.Prompt)
	}
	spawn := s.spawnCoordinator
	if spawn == nil {
		spawn = agent.SpawnHeraCoordinator
	}
	res, err := spawn(s.db, s.runner, agent.HeraCoordinatorSpawnInput{
		OrchestratorBaseName: base,
		CoordRoleName:        "coord",
		TaskPrompt:           agent.HeraCoordinatorOrientation(base) + "\n\n---\n\n" + req.Prompt,
		RolePrompt:           req.Prompt,
		Project:              req.Project,
		Branch:               req.BaseBranch,
		Backend:              req.Backend,
		Model:                req.Model,
		Archetype:            req.Archetype,
		Profile:              req.Profile,
	})
	if err != nil {
		if relErr := s.db.ReleaseHeraCoordinatorRequest(req.RequestID); relErr != nil {
			uxlog.Log("[api-hera] create coordinator: receipt release failed request=%s: %v", req.RequestID, relErr)
		}
		uxlog.Log("[api-hera] create coordinator failed project=%s request=%s: %v", req.Project, req.RequestID, err)
		status := http.StatusInternalServerError
		if errors.Is(err, agent.ErrAccount) {
			status = http.StatusBadRequest
		}
		writeErr(w, status, "failed to create coordinator", err)
		return
	}

	resp := heraCoordCreateResp{
		OrchestratorID: res.Orchestrator.ID,
		RoleID:         res.Role.ID,
		BindingID:      res.Binding.ID,
		TaskID:         res.Task.ID,
		Name:           res.Orchestrator.Name,
		BaseBranch:     req.BaseBranch,
		Profile:        req.Profile,
		Archetype:      res.Role.Archetype,
	}
	if resp.BaseBranch == "" {
		resp.BaseBranch = heraCoordDefaultBranch(proj)
	}
	if resp.Profile == "" {
		resp.Profile = proj.ResolveProfileName()
	}
	body, _ := json.Marshal(resp)
	if err := s.db.CompleteHeraCoordinatorRequest(req.RequestID, string(body)+"\n"); err != nil {
		uxlog.Log("[api-hera] create coordinator: receipt complete failed request=%s task=%s: %v", req.RequestID, res.Task.ID, err)
	}
	uxlog.Log("[api-hera] create coordinator ok: orch=%d role=%d binding=%d task=%s request=%s", resp.OrchestratorID, resp.RoleID, resp.BindingID, resp.TaskID, req.RequestID)
	events.Emit(model.EventTypeHeraOrchestratorCreated, resp.TaskID, heraCoordCreatedEvent{
		OrchestratorID: resp.OrchestratorID, RoleID: resp.RoleID, TaskID: resp.TaskID,
	})
	writeJSON(w, http.StatusCreated, resp)
}
