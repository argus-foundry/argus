package api

import (
	"context"
	"log/slog"
	"math"
	"net/http"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
	"github.com/shirou/gopsutil/v4/process"
)

// contextFamily resolves the task's backend family without usage-based spawn
// routing, which would ask this server for readings.
func (s *Server) contextFamily(task *model.Task) (*agent.Family, config.Config) {
	cfg := s.db.Config()
	name := task.Backend
	if name == "" {
		name = cfg.Projects[task.Project].Backend
	}
	if name == "" {
		name = cfg.Defaults.Backend
	}
	b, ok := cfg.Backends[name]
	if !ok {
		return &agent.Family{}, cfg
	}
	return agent.FamilyOf(b.Command), cfg
}

func (s *Server) serveReaderContext(w http.ResponseWriter, task *model.Task, fam *agent.Family, cfg config.Config, out TaskContext) {
	out.Provider, out.Source = fam.ContextProvider, fam.ContextSource
	rd, ok, err := s.readReaderContext(task, fam, cfg)
	if err != nil {
		uxlog.Log("[context] read task=%s: %v", task.ID, err)
		slog.Warn("[context] read failed", "task", task.ID, "err", err)
	}
	if err != nil || !ok {
		writeJSON(w, http.StatusOK, out)
		return
	}
	out.Provider, out.Source, out.SessionID = rd.Provider, rd.Source, rd.SessionID
	out.Live = s.contextSessionLive(task.ID)
	at := rd.MeasuredAt.UTC()
	out.MeasuredAt = &at
	out.Context = readerUsage(rd)
	writeJSON(w, http.StatusOK, out)
}

// readReaderContext rejects a previous conversation while a fresh launch has
// no captured ID. A pinned ID can legitimately resume a much older conversation.
func (s *Server) readReaderContext(task *model.Task, fam *agent.Family, cfg config.Config) (agent.ContextReading, bool, error) {
	rd, ok, err := fam.ContextReader(task, cfg)
	if err != nil || !ok {
		return rd, ok, err
	}
	matches, err := s.readerSessionMatchesLaunch(task, rd.SessionID, rd.SessionStartedAt)
	if err != nil || !matches {
		return agent.ContextReading{}, false, err
	}
	return rd, true, nil
}

// readerSessionMatchesLaunch permits pinned resumes and rejects uncaptured old launches.
func (s *Server) readerSessionMatchesLaunch(task *model.Task, sessionID string, sessionStartedAt time.Time) (bool, error) {
	if task.SessionID != "" || s.runner == nil {
		return true, nil
	}
	sess := s.runner.Get(task.ID)
	if sess == nil || !sess.Alive() {
		return true, nil
	}
	if s.runner.HasPendingRestart(task.ID) {
		return false, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	p, err := process.NewProcessWithContext(ctx, int32(sess.PID()))
	if err != nil {
		return false, err
	}
	started, err := p.CreateTimeWithContext(ctx)
	if err != nil {
		return false, err
	}
	if started <= 0 || sessionStartedAt.IsZero() || sessionStartedAt.Before(time.UnixMilli(started)) {
		uxlog.Log("[context] skip previous launch task=%s session=%s", task.ID, sessionID)
		return false, nil
	}
	return true, nil
}

// contextSessionLive reports whether a running, non-restarting process owns the task.
func (s *Server) contextSessionLive(id string) bool {
	if s.runner == nil {
		return false
	}
	sess := s.runner.Get(id)
	return sess != nil && sess.Alive() && !s.runner.HasPendingRestart(id)
}

func readerUsage(rd agent.ContextReading) *ContextUsage {
	used := rd.UsedTokens
	u := &ContextUsage{UsedTokens: &used}
	if rd.WindowTokens > 0 {
		w := rd.WindowTokens
		p := math.Round(float64(used)/float64(w)*1000) / 10
		u.WindowTokens, u.Percent = &w, &p
	}
	return u
}

// emitReaderContextChanges announces a fresh measurement for each running
// reader-backed task whose measurement time advanced since the last tick.
func (s *Server) emitReaderContextChanges(state *idleWatcherState, running []string) {
	if state.contextAt == nil {
		state.contextAt = make(map[string]time.Time)
	}
	live := make(map[string]bool, len(running))
	for _, id := range running {
		task, err := s.db.Get(id)
		if err != nil || task == nil {
			continue
		}
		fam, cfg := s.contextFamily(task)
		if fam.ContextReader == nil {
			continue
		}
		live[id] = true
		rd, ok, err := s.readReaderContext(task, fam, cfg)
		if err != nil {
			uxlog.Log("[context] tick task=%s: %v", id, err)
			slog.Warn("[context] tick read failed", "task", id, "err", err)
			continue
		}
		if !ok || !rd.MeasuredAt.After(state.contextAt[id]) {
			continue
		}
		state.contextAt[id] = rd.MeasuredAt
		events.Emit(model.EventTypeTaskContextChanged, id, map[string]string{
			"session_id":  rd.SessionID,
			"measured_at": rd.MeasuredAt.UTC().Format(time.RFC3339Nano),
		})
	}
	for id := range state.contextAt {
		if !live[id] {
			delete(state.contextAt, id)
		}
	}
}
