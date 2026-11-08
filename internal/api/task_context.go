package api

import (
	"net/http"
	"time"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/bridge"
)

const (
	contextProviderAnthropic = "anthropic"
	contextSourceBridge      = "bridge"
)

// TaskContext is the backend-neutral response of GET /api/tasks/{id}/context.
// Nil pointers are unknown, never zero.
type TaskContext struct {
	TaskID     string        `json:"task_id"`
	SessionID  string        `json:"session_id"`
	Provider   string        `json:"provider"`
	Live       bool          `json:"live"`
	MeasuredAt *time.Time    `json:"measured_at"`
	Source     string        `json:"source"`
	Context    *ContextUsage `json:"context"`
}

// ContextUsage is the measured fill of one task's context window.
type ContextUsage struct {
	UsedTokens   *int64          `json:"used_tokens"`
	WindowTokens *int64          `json:"window_tokens"`
	Percent      *float64        `json:"percent"`
	Categories   ContextCategory `json:"categories"`
}

// ContextCategory carries aggregate token counts only; every key is always present.
type ContextCategory struct {
	Messages   *int64 `json:"messages"`
	Memory     *int64 `json:"memory"`
	Tools      *int64 `json:"tools"`
	Agents     *int64 `json:"agents"`
	Skills     *int64 `json:"skills"`
	System     *int64 `json:"system"`
	Compaction *int64 `json:"compaction"`
}

// handleTaskContext serves GET /api/tasks/{id}/context. Without a measurement
// it answers 200 with live=false and context=null.
func (s *Server) handleTaskContext(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.db.Get(id)
	if err != nil || task == nil {
		writeErr(w, http.StatusNotFound, "task not found", nil)
		return
	}
	out := TaskContext{TaskID: id, Provider: contextProviderAnthropic, Source: contextSourceBridge}
	if fam, cfg := s.contextFamily(task); fam.ContextReader != nil {
		s.serveReaderContext(w, task, fam, cfg, out)
		return
	}
	now := time.Now()
	st, live := s.bridgeState(id, now)
	if !live && s.bridgeHub != nil {
		st, _ = s.bridgeHub.Get(id)
	}
	if st.Context == nil || st.LastMeasureAt.IsZero() || s.contextSessionReplaced(id, st) {
		writeJSON(w, http.StatusOK, out)
		return
	}
	at := st.LastMeasureAt.UTC()
	out.SessionID, out.Live, out.MeasuredAt, out.Context = st.SessionID, live, &at, contextUsageOf(st.Context)
	writeJSON(w, http.StatusOK, out)
}

// contextSessionReplaced reports whether a live process of a different launch
// than the recorded measurement now owns the task.
func (s *Server) contextSessionReplaced(id string, st bridge.State) bool {
	if s.runner == nil {
		return false
	}
	sess := s.runner.Get(id)
	if sess == nil || !sess.Alive() {
		return false
	}
	gen := agent.SessionBridgeGeneration(sess)
	return gen != "" && st.Generation != "" && gen != st.Generation
}

func contextUsageOf(c *bridge.Context) *ContextUsage {
	u := &ContextUsage{UsedTokens: c.Tokens, Percent: c.Percent}
	if c.Window > 0 {
		w := c.Window
		u.WindowTokens = &w
	}
	if cc := c.Categories; cc != nil {
		u.Categories = ContextCategory{
			Messages: cc.Messages, Memory: cc.Memory, Tools: cc.Tools, Agents: cc.Agents,
			Skills: cc.Skills, System: cc.System, Compaction: cc.Compaction,
		}
	}
	return u
}
