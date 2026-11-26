package api

import (
	"context"
	"errors"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/app/agentview"
	"github.com/argus-foundry/argus/internal/bridge"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
	"github.com/argus-foundry/argus/internal/voice"
)

// VoiceOperator is the voice command backend. It reuses status counting,
// stop, resume, and session input.
func (s *Server) VoiceOperator() voice.Operator {
	return apiVoice{s: s, create: func(input agent.CreateInput) (*model.Task, error) {
		task, _, err := agent.CreateAndStart(s.db, s.runner, input)
		return task, err
	}}
}

type apiVoice struct {
	s      *Server
	create func(agent.CreateInput) (*model.Task, error)
}

func (a apiVoice) Snapshot(ctx context.Context, now time.Time) (voice.Snapshot, error) {
	if err := ctx.Err(); err != nil {
		return voice.Snapshot{}, &voice.OpError{Speech: "That was canceled.", Code: "canceled"}
	}
	snap, err := a.s.voiceSnapshot(now)
	if err != nil {
		return voice.Snapshot{}, err
	}
	c := snap.Counts
	uxlog.Log("[voice] brief running=%d idle=%d needs_input=%d blocked=%d finished=%d",
		c.Running, c.Idle, c.NeedsInput, c.Blocked, c.RecentlyFinished)
	slog.Info("[voice] brief", "running", c.Running, "idle", c.Idle, "needs_input", c.NeedsInput, "blocked", c.Blocked, "finished", c.RecentlyFinished)
	return snap, nil
}

func (a apiVoice) Find(ctx context.Context, taskID, name, project string) (voice.FindResult, error) {
	if err := ctx.Err(); err != nil {
		return voice.FindResult{}, &voice.OpError{Speech: "That was canceled.", Code: "canceled"}
	}
	taskID = strings.TrimSpace(taskID)
	name = strings.TrimSpace(name)
	project = strings.TrimSpace(project)
	if taskID != "" {
		t, err := a.s.db.Get(taskID)
		if err != nil {
			return voice.FindResult{}, err
		}
		if t == nil {
			return voice.FindResult{}, nil
		}
		return voice.FindResult{Found: true, Ref: a.ref(t)}, nil
	}
	tasks, err := a.s.db.Tasks()
	if err != nil {
		return voice.FindResult{}, err
	}
	spokenAlias := spokenWorkflowAlias(name)
	var matches, open []*model.Task
	for _, t := range tasks {
		if t == nil || t.IsResident() || t.Kind != "" {
			continue
		}
		if project != "" && !strings.EqualFold(t.Project, project) {
			continue
		}
		if !strings.EqualFold(spokenWorkflowAlias(t.Name), spokenAlias) {
			continue
		}
		matches = append(matches, t)
		if !t.Archived {
			open = append(open, t)
		}
	}
	if len(open) > 0 {
		matches = open
	}
	switch len(matches) {
	case 0:
		return voice.FindResult{}, nil
	case 1:
		return voice.FindResult{Found: true, Ref: a.ref(matches[0])}, nil
	default:
		return voice.FindResult{Ambiguous: true, Matches: len(matches)}, nil
	}
}

func spokenWorkflowAlias(name string) string {
	return strings.Join(strings.Fields(name), "-")
}

func (a apiVoice) Create(ctx context.Context, name, project, prompt string) (voice.TaskRef, error) {
	if err := ctx.Err(); err != nil {
		return voice.TaskRef{}, &voice.OpError{Speech: "That was canceled.", Code: "canceled"}
	}
	a.s.voiceCreateMu.Lock()
	defer a.s.voiceCreateMu.Unlock()
	existing, err := a.Find(ctx, "", name, project)
	if err != nil {
		return voice.TaskRef{}, err
	}
	if existing.Found || existing.Ambiguous {
		return existing.Ref, &voice.OpError{Speech: voice.SpeakName(name) + " already exists.", Code: "exists"}
	}
	if agent.SafeName(name) != spokenWorkflowAlias(name) {
		return voice.TaskRef{}, &voice.OpError{Speech: "Use a shorter workflow name with words separated by spaces.", Code: "unstable_name"}
	}
	if err := ctx.Err(); err != nil {
		return voice.TaskRef{}, &voice.OpError{Speech: "That was canceled.", Code: "canceled"}
	}
	task, err := a.create(agent.CreateInput{
		Name: name, DisplayName: spokenWorkflowAlias(name), Prompt: prompt, Project: project,
	})
	if err != nil {
		slog.Error("[voice] start failed", "name", name, "project", project, "err", err)
		uxlog.Log("[voice] start failed name=%q project=%q err=%v", name, project, err)
		if errors.Is(err, agent.ErrStartAmbiguous) {
			return voice.TaskRef{}, voiceLaunchAmbiguous(voice.SpeakName(name))
		}
		return voice.TaskRef{}, &voice.OpError{Speech: "I couldn't start " + voice.SpeakName(name) + ".", Code: "create_failed"}
	}
	slog.Info("[voice] started", "task", task.ID, "name", task.Name, "project", task.Project)
	uxlog.Log("[voice] started task=%s name=%q project=%q", task.ID, task.Name, task.Project)
	return a.ref(task), nil
}

func (a apiVoice) Stop(ctx context.Context, taskID string) error {
	unlock, err := a.s.lockTaskMutation(ctx, taskID)
	if err != nil {
		return &voice.OpError{Speech: "That was canceled.", Code: "canceled"}
	}
	defer unlock()
	unlockSession, err := a.lockSessionMutation(taskID)
	if err != nil {
		return err
	}
	defer unlockSession()
	t, err := a.s.db.Get(taskID)
	if err != nil || t == nil {
		return &voice.OpError{Speech: "I can't find that workflow.", Code: "not_found"}
	}
	if t.Archived {
		return &voice.OpError{Speech: voice.SpeakName(t.Name) + " is archived.", Code: "archived"}
	}
	if t.Status == model.StatusPending {
		// The task row is visible before the launch finishes. A pending
		// row can already have a PTY, but its start still owns the status.
		if !a.s.voiceCreateMu.TryLock() {
			return &voice.OpError{Speech: voice.SpeakName(t.Name) + " is still starting.", Code: "agent_starting"}
		}
		defer a.s.voiceCreateMu.Unlock()
	}
	if err := a.s.stopTaskSession(t, true); err != nil {
		if errors.Is(err, agent.ErrSessionStarting) {
			return &voice.OpError{Speech: voice.SpeakName(t.Name) + " is starting or restarting.", Code: "agent_restarting"}
		}
		if errors.Is(err, agent.ErrSessionNotFound) {
			return &voice.OpError{Speech: "I couldn't find a live session to stop.", Code: "no_session"}
		}
		slog.Error("[voice] stop failed", "task", taskID, "err", err)
		uxlog.Log("[voice] stop failed task=%s err=%v", taskID, err)
		return &voice.OpError{Speech: "I couldn't stop " + voice.SpeakName(t.Name) + ".", Code: "stop_failed"}
	}
	slog.Info("[voice] stopped", "task", taskID)
	uxlog.Log("[voice] stopped task=%s", taskID)
	return nil
}

func (a apiVoice) Resume(ctx context.Context, taskID string, force bool) error {
	unlock, err := a.s.lockTaskMutation(ctx, taskID)
	if err != nil {
		return &voice.OpError{Speech: "That was canceled.", Code: "canceled"}
	}
	defer unlock()
	unlockSession, err := a.lockSessionMutation(taskID)
	if err != nil {
		return err
	}
	defer unlockSession()
	task, err := a.s.db.Get(taskID)
	if err != nil || task == nil {
		return &voice.OpError{Speech: "I can't find that workflow.", Code: "not_found"}
	}
	spoken := voice.SpeakName(task.Name)
	if task.Archived {
		return &voice.OpError{Speech: spoken + " is archived.", Code: "archived"}
	}
	if sess := a.s.runner.Get(task.ID); sess != nil && !sess.IsIdle() {
		return &voice.OpError{Speech: spoken + " is already running.", Code: "task_running"}
	}
	if a.s.runner.HasPendingRestart(task.ID) {
		uxlog.Log("[voice] resume refused pending restart task=%s", taskID)
		return &voice.OpError{Speech: spoken + " is restarting.", Code: "agent_restarting"}
	}
	gate := a.s.evaluateCloseout(task, force, "voice")
	if !gate.proceed {
		if gate.code == "hera_closeout" {
			return &voice.OpError{Speech: spoken + " is closed out. Confirm to revive it.", Code: gate.code}
		}
		return &voice.OpError{Speech: "I couldn't resume " + spoken + ".", Code: gate.code}
	}
	if _, err := a.s.startResumedSession(task, "voice"); err != nil {
		ambiguous := errors.Is(err, agent.ErrStartAmbiguous)
		if gate.rollback != nil && !ambiguous {
			gate.rollback()
		}
		slog.Error("[voice] resume failed", "task", taskID, "err", err)
		uxlog.Log("[voice] resume failed task=%s err=%v", taskID, err)
		if ambiguous {
			return voiceLaunchAmbiguous(spoken)
		}
		return &voice.OpError{Speech: "I couldn't resume " + spoken + ".", Code: "start_failed"}
	}
	slog.Info("[voice] resumed", "task", taskID)
	uxlog.Log("[voice] resumed task=%s", taskID)
	return nil
}

func (a apiVoice) lockSessionMutation(taskID string) (func(), error) {
	unlock, err := a.s.tryLockExistingSessionMutation(taskID)
	switch {
	case errors.Is(err, db.ErrTaskNotFound):
		return nil, &voice.OpError{Speech: "I can't find that workflow.", Code: "not_found"}
	case errors.Is(err, agent.ErrSessionStarting):
		return nil, &voice.OpError{Speech: "The agent is starting or restarting.", Code: "agent_restarting"}
	case err != nil:
		uxlog.Log("[voice] session operation lock failed task=%s err=%v", taskID, err)
		return nil, &voice.OpError{Speech: "I couldn't coordinate a session operation.", Code: "session_lock_failed"}
	}
	return unlock, nil
}

func voiceLaunchAmbiguous(spoken string) *voice.OpError {
	return &voice.OpError{Speech: "Check " + spoken + " before retrying because it may still be starting.", Code: "start_ambiguous"}
}

func (a apiVoice) Send(ctx context.Context, taskID, text string) error {
	unlock, err := a.s.lockTaskMutation(ctx, taskID)
	if err != nil {
		return &voice.OpError{Speech: "That was canceled.", Code: "canceled"}
	}
	defer unlock()
	t, err := a.s.db.Get(taskID)
	if err != nil || t == nil {
		return &voice.OpError{Speech: "I can't find that workflow.", Code: "not_found"}
	}
	if t.Archived {
		return &voice.OpError{Speech: voice.SpeakName(t.Name) + " is archived.", Code: "archived"}
	}
	sess := a.s.runner.Get(taskID)
	if sess == nil {
		return &voice.OpError{Speech: voice.SpeakName(t.Name) + " has no live session.", Code: "no_session"}
	}
	if !strings.HasSuffix(text, "\n") && !strings.HasSuffix(text, "\r") {
		text += "\r"
	}
	write := sess.WriteInput
	if acknowledged, ok := sess.(interface {
		WriteInputAcknowledged(context.Context, []byte, agentview.InputOrigin) (int, error)
	}); ok {
		write = func(data []byte, origin agentview.InputOrigin) (int, error) {
			return acknowledged.WriteInputAcknowledged(ctx, data, origin)
		}
	}
	if _, err := write([]byte(text), agentview.OriginUser); err != nil {
		slog.Error("[voice] input failed", "task", taskID, "err", err)
		uxlog.Log("[voice] input failed task=%s err=%v", taskID, err)
		return &voice.OpError{Speech: "I couldn't confirm delivery.", Code: "send_failed"}
	}
	slog.Info("[voice] input", "task", taskID, "bytes", len(text))
	uxlog.Log("[voice] input task=%s bytes=%d", taskID, len(text))
	return nil
}

func (a apiVoice) ref(t *model.Task) voice.TaskRef {
	ref := voice.TaskRef{
		ID: t.ID, Name: t.Name, Project: t.Project, Status: t.Status.String(), Archived: t.Archived,
	}
	if a.s.runner == nil {
		return ref
	}
	if sess := a.s.runner.Get(t.ID); sess != nil {
		ref.Live = true
		ref.Idle = sess.IsIdle()
	}
	if a.s.runner.HasPendingRestart(t.ID) {
		ref.Live = true
		ref.Idle = false
	}
	for _, id := range a.s.runner.NeedsInputIDs() {
		if id == t.ID && ref.Live {
			ref.NeedsInput = true
		}
	}
	return ref
}

// voiceSnapshot builds the spoken brief and the numbers GET /api/status reports.
// Session running and idle counts stay the status endpoint's counts. The voice
// buckets split those sessions into working, idle, and needs-input.
func (s *Server) voiceSnapshot(now time.Time) (voice.Snapshot, error) {
	runningIDs, idleIDs := s.runner.RunningAndIdle()
	states := s.BridgeStates()
	idleIDs = bridge.Override(idleIDs, states, bridge.Quiescent)

	tasks, err := s.db.Tasks()
	if err != nil {
		return voice.Snapshot{}, err
	}
	runningSet := idSet(runningIDs)
	idleSet := idSet(idleIDs)
	needsSet := idSet(s.runner.NeedsInputIDs())
	for id, st := range states {
		if st.NeedsInput {
			needsSet[id] = true
		} else {
			delete(needsSet, id)
		}
	}

	var snap voice.Snapshot
	snap.SessionsRunning = len(runningIDs)
	snap.SessionsIdle = len(idleIDs)
	byID := make(map[string]*model.Task, len(tasks))
	var runNames, idleNames, needNames, doneNames []string
	needIDs := map[string]bool{}
	for _, t := range tasks {
		if t == nil {
			continue
		}
		byID[t.ID] = t
		if t.Archived {
			continue
		}
		switch t.Status {
		case model.StatusPending:
			snap.Counts.Pending++
		case model.StatusInProgress:
			snap.Counts.InProgress++
		case model.StatusInReview:
			snap.Counts.InReview++
		case model.StatusComplete:
			snap.Counts.Complete++
		}
		live := runningSet[t.ID]
		needs := live && t.Status == model.StatusInProgress && needsSet[t.ID]
		switch {
		case needs:
			needNames = append(needNames, t.Name)
			needIDs[t.ID] = true
		case live && idleSet[t.ID]:
			idleNames = append(idleNames, t.Name)
		case live:
			runNames = append(runNames, t.Name)
		}
		if t.Status == model.StatusComplete && !t.EndedAt.IsZero() {
			age := now.Sub(t.EndedAt)
			if age >= 0 && age <= voice.RecentWindow {
				doneNames = append(doneNames, t.Name)
			}
		}
	}
	blockedNames := blockedNames(s, byID, needIDs)
	sort.Strings(runNames)
	sort.Strings(idleNames)
	sort.Strings(needNames)
	sort.Strings(blockedNames)
	sort.Strings(doneNames)
	snap.Running = runNames
	snap.Idle = idleNames
	snap.NeedsInput = needNames
	snap.Blocked = blockedNames
	snap.RecentlyFinished = doneNames
	snap.Counts.Running = len(runNames)
	snap.Counts.Idle = len(idleNames)
	snap.Counts.NeedsInput = len(needNames)
	snap.Counts.Blocked = len(blockedNames)
	snap.Counts.RecentlyFinished = len(doneNames)
	snap.Counts.SessionsRunning = snap.SessionsRunning
	snap.Counts.SessionsIdle = snap.SessionsIdle
	return snap, nil
}

func blockedNames(s *Server, byID map[string]*model.Task, needs map[string]bool) []string {
	rows, err := s.db.ListBlockedHeraRoleBindings()
	if err != nil {
		slog.Warn("[voice] blocked list failed", "err", err)
		uxlog.Log("[voice] blocked list failed err=%v", err)
		return nil
	}
	seen := map[string]bool{}
	var names []string
	for _, row := range rows {
		t := byID[row.TaskID]
		if t == nil || t.Archived || seen[t.ID] || needs[t.ID] {
			continue
		}
		seen[t.ID] = true
		names = append(names, t.Name)
	}
	return names
}

func idSet(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}
