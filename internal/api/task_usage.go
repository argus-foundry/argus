package api

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/uxlog"
)

const (
	usageDefaultLimit = 50
	usageMaxLimit     = 200
)

// TaskUsage is the response of GET /api/tasks/{id}/usage. Nil counters are
// unknown, never zero; input_tokens includes cached_input_tokens.
type TaskUsage struct {
	TaskID     string       `json:"task_id"`
	SessionID  string       `json:"session_id"`
	Provider   string       `json:"provider"`
	MeasuredAt *time.Time   `json:"measured_at"`
	Source     string       `json:"source"`
	Totals     *UsageTotals `json:"totals"`
	Turns      []UsageTurn  `json:"turns"`
	NextCursor *string      `json:"next_cursor"`
}

// UsageTotals are session-level token counters.
type UsageTotals struct {
	InputTokens       *int64 `json:"input_tokens"`
	CachedInputTokens *int64 `json:"cached_input_tokens"`
	OutputTokens      *int64 `json:"output_tokens"`
}

// UsageTurn is one turn's counters, listed newest first.
type UsageTurn struct {
	TurnID            string `json:"turn_id"`
	InputTokens       *int64 `json:"input_tokens"`
	CachedInputTokens *int64 `json:"cached_input_tokens"`
	OutputTokens      *int64 `json:"output_tokens"`
}

// handleTaskUsage serves GET /api/tasks/{id}/usage. Unsupported telemetry or
// no measurement yet answers 200 with totals=null and turns=[].
func (s *Server) handleTaskUsage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	task, err := s.db.Get(id)
	if err != nil || task == nil {
		writeErr(w, http.StatusNotFound, "task not found", nil)
		return
	}
	q := r.URL.Query()
	limit := usageDefaultLimit
	if v := q.Get("limit"); v != "" {
		n, perr := strconv.Atoi(v)
		if perr != nil || n < 1 {
			writeErr(w, http.StatusBadRequest, "invalid limit", nil)
			return
		}
		limit = min(n, usageMaxLimit)
	}
	var cur usageCursor
	if v := q.Get("before"); v != "" {
		c, ok := parseUsageCursor(v)
		if !ok {
			writeErr(w, http.StatusBadRequest, "invalid cursor", nil)
			return
		}
		cur = c
	}
	fam, _ := s.contextFamily(task)
	out := TaskUsage{TaskID: id, Provider: fam.UsageProvider, Source: fam.UsageSource, Turns: []UsageTurn{}}
	if out.Provider == "" {
		out.Provider = contextProviderAnthropic
	}
	if out.Source == "" {
		out.Source = contextSourceBridge
	}
	current, _, rerr := s.refreshTaskUsage(task)
	if rerr != nil {
		writeErr(w, http.StatusInternalServerError, "usage store failure", nil)
		return
	}
	sid := q.Get("session_id")
	if task.SessionID != "" {
		current = task.SessionID
	}
	out.SessionID = current
	var sess *db.UsageSession
	if sid != "" {
		sess, err = s.db.TaskUsageSession(id, sid)
	} else if current != "" {
		sess, err = s.db.TaskUsageSession(id, current)
	} else if matches, _ := s.readerSessionMatchesLaunch(task, "", time.Time{}); matches {
		sess, err = s.db.LatestTaskUsageSession(id)
	}
	if err != nil {
		uxlog.Log("[usage] session read task=%s kind=%s", id, usageErrorKind(err))
		slog.Warn("[usage] session read failed", "task", strconv.Quote(id), "kind", usageErrorKind(err))
		writeErr(w, http.StatusInternalServerError, "usage store failure", nil)
		return
	}
	if sess == nil {
		if sid != "" && sid != task.SessionID {
			writeErr(w, http.StatusNotFound, "session not found", nil)
			return
		}
		if sid != "" {
			out.SessionID = sid
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	if fam.UsageReader == nil {
		if sid != "" {
			out.SessionID = sid
		}
		writeJSON(w, http.StatusOK, out)
		return
	}
	rows, err := s.db.TaskUsageTurns(id, sess.SessionID, limit+1, cur.set, cur.ns, cur.id)
	if err != nil {
		uxlog.Log("[usage] turns read task=%s kind=%s", id, usageErrorKind(err))
		slog.Warn("[usage] turns read failed", "task", strconv.Quote(id), "kind", usageErrorKind(err))
		writeErr(w, http.StatusInternalServerError, "usage store failure", nil)
		return
	}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		c := encodeUsageCursor(last.StartedAt.UnixNano(), last.TurnID)
		out.NextCursor = &c
	}
	for _, t := range rows {
		out.Turns = append(out.Turns, UsageTurn{TurnID: t.TurnID, InputTokens: t.InputTokens, CachedInputTokens: t.CachedInputTokens, OutputTokens: t.OutputTokens})
	}
	at := sess.MeasuredAt.UTC()
	out.SessionID, out.Provider, out.Source, out.MeasuredAt = sess.SessionID, sess.Provider, sess.Source, &at
	out.Totals = &UsageTotals{InputTokens: sess.InputTokens, CachedInputTokens: sess.CachedInputTokens, OutputTokens: sess.OutputTokens}
	writeJSON(w, http.StatusOK, out)
}

type usageCursor struct {
	set bool
	ns  int64
	id  string
}

func encodeUsageCursor(ns int64, id string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d:%s", ns, id)))
}

func parseUsageCursor(v string) (usageCursor, bool) {
	raw, err := base64.RawURLEncoding.DecodeString(v)
	if err != nil {
		return usageCursor{}, false
	}
	ns, id, ok := strings.Cut(string(raw), ":")
	n, perr := strconv.ParseInt(ns, 10, 64)
	if !ok || perr != nil || id == "" {
		return usageCursor{}, false
	}
	return usageCursor{set: true, ns: n, id: id}, true
}

// refreshTaskUsage ingests the task's current measurement into the store. A
// reader failure is logged and keeps the stored counters; it never writes
// zeros. current identifies a validated session even before it has measurements.
func (s *Server) refreshTaskUsage(task *model.Task) (current string, changed bool, err error) {
	fam, cfg := s.contextFamily(task)
	if fam.UsageReader == nil {
		return "", false, nil
	}
	rd, ok, rerr := fam.UsageReader(task, cfg)
	if rerr == nil && (ok || rd.SessionID != "") {
		matches, err := s.readerSessionMatchesLaunch(task, rd.SessionID, rd.SessionStartedAt)
		if err == nil && !matches {
			return "", false, nil
		}
		rerr = err
	}
	if rerr != nil {
		uxlog.Log("[usage] read task=%s kind=%s", task.ID, usageErrorKind(rerr))
		slog.Warn("[usage] read failed", "task", strconv.Quote(task.ID), "kind", usageErrorKind(rerr))
		return "", false, nil
	}
	if !ok {
		return rd.SessionID, false, nil
	}
	turns := make([]db.UsageTurn, 0, len(rd.Turns))
	for _, t := range rd.Turns {
		turns = append(turns, db.UsageTurn{TurnID: t.ID, StartedAt: t.StartedAt, MeasuredAt: t.MeasuredAt, UsageCounters: db.UsageCounters(t.UsageCounters)})
	}
	changed, err = s.db.UpsertTaskUsage(db.UsageSession{
		TaskID: task.ID, SessionID: rd.SessionID, Account: task.Account, Provider: rd.Provider, Source: rd.Source,
		MeasuredAt: rd.MeasuredAt, UsageCounters: db.UsageCounters(rd.Totals),
	}, turns)
	if err != nil {
		uxlog.Log("[usage] store task=%s kind=%s", task.ID, usageErrorKind(err))
		slog.Warn("[usage] store failed", "task", strconv.Quote(task.ID), "kind", usageErrorKind(err))
		return "", false, err
	}
	if changed {
		payload := map[string]string{"session_id": rd.SessionID, "measured_at": rd.MeasuredAt.UTC().Format(time.RFC3339Nano)}
		if turns, terr := s.db.TaskUsageTurns(task.ID, rd.SessionID, 1, false, 0, ""); terr == nil && len(turns) == 1 {
			payload["turn_id"] = turns[0].TurnID
		}
		events.Emit(model.EventTypeTaskUsageChanged, task.ID, payload)
		uxlog.Log("[usage] changed task=%s session=%s", task.ID, rd.SessionID)
		slog.Info("[usage] changed", "task", strconv.Quote(task.ID), "session", strconv.Quote(rd.SessionID))
	}
	return rd.SessionID, changed, nil
}

// emitReaderUsageChanges polls running tasks; every changed ingestion publishes.
func (s *Server) emitReaderUsageChanges(_ *idleWatcherState, running []string) {
	for _, id := range running {
		task, err := s.db.Get(id)
		if err != nil || task == nil {
			continue
		}
		_, _, _ = s.refreshTaskUsage(task)
	}
}

func usageErrorKind(err error) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return "permission_denied"
	case errors.Is(err, fs.ErrNotExist):
		return "not_found"
	case errors.Is(err, fs.ErrClosed):
		return "closed"
	default:
		return "unavailable"
	}
}
