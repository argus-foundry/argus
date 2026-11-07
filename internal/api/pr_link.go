package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/gitutil"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

type prLinkJSON struct {
	Source    model.PRSource    `json:"source"`
	URL       string            `json:"url"`
	State     string            `json:"state"`
	Lifecycle model.PRLifecycle `json:"lifecycle"`
	CheckedAt *string           `json:"checked_at"`
}

type prLinkReq struct {
	URL string `json:"url"`
}

// prLinkProjection reads the manual association and the derived pr cache; it
// never shells out to gh.
func (s *Server) prLinkProjection(id string) (prLinkJSON, error) {
	link, err := s.db.ManualPR(id)
	if err != nil {
		return prLinkJSON{}, err
	}
	entries, err := s.db.ListMeta(id, "pr")
	if err != nil {
		return prLinkJSON{}, err
	}
	var state, lifecycle, url string
	var checked time.Time
	for _, e := range entries {
		switch e.Key {
		case "state":
			state = e.Value
		case "lifecycle":
			lifecycle = e.Value
		case "url":
			url = e.Value
		}
		if e.UpdatedAt.After(checked) {
			checked = e.UpdatedAt
		}
	}
	out := prLinkJSON{Source: model.PRSourceAutomatic, URL: url, State: state, Lifecycle: model.ResolvePRLifecycle(state, lifecycle)}
	if link != "" {
		out.Source = model.PRSourceManual
		out.URL = link
		if state == "" {
			out.State = model.PRUnknown.String()
		}
	} else if state == "" {
		out.State = model.PRNone.String()
	}
	if !checked.IsZero() {
		ts := checked.UTC().Format(time.RFC3339)
		out.CheckedAt = &ts
	}
	return out, nil
}

func (s *Server) writePRLink(w http.ResponseWriter, id string) {
	p, err := s.prLinkProjection(id)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) taskExists(w http.ResponseWriter, id string) bool {
	task, err := s.db.Get(id)
	if err != nil || task == nil {
		writeErr(w, http.StatusNotFound, "task not found", nil)
		return false
	}
	return true
}

func (s *Server) handleGetTaskPR(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.taskExists(w, id) {
		return
	}
	s.writePRLink(w, id)
}

func (s *Server) handlePutTaskPR(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.taskExists(w, id) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4*1024)
	var req prLinkReq
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid JSON", err)
		return
	}
	canonical, err := gitutil.ParsePRURL(req.URL)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "url must be a https://github.com/<owner>/<repo>/pull/<n> pull request url", nil)
		return
	}
	if err := s.db.SetManualPR(id, canonical); err != nil {
		if errors.Is(err, db.ErrTaskNotFound) {
			writeErr(w, http.StatusNotFound, "task not found", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	events.Emit(model.EventTypeTaskPRChanged, id, model.PRChangedPayload{
		URL: canonical, State: model.PRUnknown.String(), Lifecycle: model.PRLifecycleUnknown, Source: model.PRSourceManual,
	})
	uxlog.Log("[pr] api: task=%s manual link set", id)
	s.writePRLink(w, id)
}

func (s *Server) handleDeleteTaskPR(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.taskExists(w, id) {
		return
	}
	if err := s.db.ClearManualPR(id); err != nil {
		if errors.Is(err, db.ErrTaskNotFound) {
			writeErr(w, http.StatusNotFound, "task not found", nil)
			return
		}
		writeErr(w, http.StatusInternalServerError, "", err)
		return
	}
	events.Emit(model.EventTypeTaskPRChanged, id, model.PRChangedPayload{
		State: model.PRNone.String(), Lifecycle: model.PRLifecycleUnknown, Source: model.PRSourceAutomatic,
	})
	uxlog.Log("[pr] api: task=%s manual link cleared", id)
	s.writePRLink(w, id)
}
