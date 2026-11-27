package api

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/uxlog"
)

type decisionCredential struct {
	hash    [32]byte
	role    int64
	expires time.Time
	session bool
}
type decisionStore struct {
	mu      sync.Mutex
	entries []decisionCredential
	now     func() time.Time
}

func (s *Server) handleDecidePage(w http.ResponseWriter, r *http.Request) {
	data, err := staticFS.ReadFile("static/decide.html")
	if err != nil {
		http.Error(w, "decision page unavailable", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Write(data) //nolint:errcheck
}

func (s *decisionStore) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}
func (s *decisionStore) sweep(now time.Time) {
	live := s.entries[:0]
	for _, e := range s.entries {
		if now.Before(e.expires) {
			live = append(live, e)
		} else {
			uxlog.Log("[decide] expired role=%d session=%t", e.role, e.session)
		}
	}
	s.entries = live
}
func (s *decisionStore) mint(role int64, session bool) (string, time.Time, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock()
	s.sweep(now)
	if len(s.entries) >= 4096 {
		return "", time.Time{}, errors.New("decision credential capacity reached")
	}
	plain, err := GenerateToken()
	if err != nil {
		return "", time.Time{}, err
	}
	ttl := 90 * time.Second
	if session {
		ttl = 8 * time.Hour
	}
	expires := now.Add(ttl)
	s.entries = append(s.entries, decisionCredential{sha256.Sum256([]byte(plain)), role, expires, session})
	return plain, expires, nil
}
func (s *decisionStore) lookup(plain string, consume bool) (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sweep(s.clock())
	hash := sha256.Sum256([]byte(plain))
	for n, e := range s.entries {
		if e.session == !consume && subtle.ConstantTimeCompare(hash[:], e.hash[:]) == 1 {
			if consume {
				s.entries = append(s.entries[:n], s.entries[n+1:]...)
			}
			return e.role, true
		}
	}
	return 0, false
}

// This branch precedes the ordinary auth middleware, including its public-path
// skips. Decision credentials can never acquire a device or plugin identity.
func (s *Server) decisionMiddleware(next http.Handler) http.Handler {
	routes := s.routes()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/hera/") && strings.Contains(r.URL.Path, "/board") {
			w.Header().Set("Cache-Control", "no-store")
		}
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			next.ServeHTTP(w, r)
			return
		}
		plain := strings.TrimPrefix(auth, "Bearer ")
		if _, ok := s.decisions.lookup(plain, false); ok {
			if !decisionSessionRoute(r.Method, r.URL.Path) {
				uxlog.Log("[decide] denied route")
				writeErr(w, http.StatusForbidden, "decision session scope", nil)
				return
			}
			r.Header.Set("X-Argus-Auth", "rail-board")
			routes.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func decodeDecisionJSON(w http.ResponseWriter, r *http.Request, v any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(v); err != nil {
		writeJSON(w, 400, map[string]string{"code": "invalid_request", "error": "invalid decision request"})
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeJSON(w, 400, map[string]string{"code": "invalid_request", "error": "expected one JSON value"})
		return false
	}
	return true
}

func (s *Server) decisionRole(w http.ResponseWriter, r *http.Request) (int64, bool) {
	role, err := strconv.ParseInt(r.PathValue("role_id"), 10, 64)
	if err != nil || role <= 0 {
		writeErr(w, 400, "invalid role id", nil)
		return 0, false
	}
	origin := authOrigin(r)
	if origin != "master" && origin != "device" && origin != "rail-board" {
		writeErr(w, 403, "decision access required", nil)
		return 0, false
	}
	entry, err := s.db.HeraRole(role)
	if err != nil {
		if errors.Is(err, db.ErrHeraNotFound) && origin == "rail-board" {
			writeCodedErr(w, 403, "board_not_visible", "Coordinator is hidden, archived or unavailable in the Hera rail")
		} else if errors.Is(err, db.ErrHeraNotFound) {
			writeCodedErr(w, 404, "board_item_not_found", "board coordinator not found")
		} else {
			uxlog.Log("[decide] role lookup failed role=%d", role)
			writeCodedErr(w, 500, "board_unavailable", "board unavailable")
		}
		return 0, false
	}
	if entry.Kind != db.HeraKindCoordinator {
		if origin == "rail-board" {
			writeCodedErr(w, 403, "board_not_visible", "Coordinator is unavailable in the Hera rail")
			return 0, false
		}
		writeCodedErr(w, 400, "invalid_request", "decisions require a coordinator")
		return 0, false
	}
	visible, err := s.decisionVisible(role)
	if err != nil {
		uxlog.Log("[decide] visibility failed role=%d", role)
		writeErr(w, 500, "board unavailable", nil)
		return 0, false
	}
	if !visible {
		uxlog.Log("[decide] visibility refused role=%d", role)
		writeCodedErr(w, 403, "board_not_visible", "Coordinator is hidden, archived or unavailable in the Hera rail")
		return 0, false
	}
	return role, true
}
func (s *Server) openDecisions(role int64) ([]*db.HeraBoardItem, error) {
	items, err := s.db.ListHeraBoardItems(role, false)
	if err != nil {
		return nil, err
	}
	out := make([]*db.HeraBoardItem, 0)
	for _, i := range items {
		if i.Kind == "decision" && i.DeferredAt == nil {
			out = append(out, i)
		}
	}
	return out, nil
}

func (s *Server) handleDecisionLink(w http.ResponseWriter, r *http.Request) {
	if origin := authOrigin(r); origin != "master" && origin != "device" {
		writeErr(w, 403, "master or device token required", nil)
		return
	}
	role, ok := s.decisionRole(w, r)
	if !ok {
		return
	}
	var p struct {
		Focus int64 `json:"focus_item_id"`
	}
	if r.ContentLength != 0 && !decodeDecisionJSON(w, r, &p, 2048) {
		return
	}
	items, err := s.db.ListHeraBoardItems(role, false)
	if err != nil {
		writeErr(w, 500, "decisions unavailable", nil)
		return
	}
	path := fmt.Sprintf("/decide?role=%d", role)
	if p.Focus != 0 {
		found := false
		for _, i := range items {
			if i.ID == p.Focus {
				found = true
			}
		}
		if !found {
			writeErr(w, 400, "focus is not an open board item", nil)
			return
		}
		path += fmt.Sprintf("&focus=%d", p.Focus)
	}
	ticket, expires, err := s.decisions.mint(role, false)
	if err != nil {
		writeErr(w, 503, "decision link unavailable", nil)
		return
	}
	uxlog.Log("[decide] minted role=%d count=%d", role, len(items))
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"path": path, "ticket": ticket, "expires_at": expires})
}
func (s *Server) handleDecisionSession(w http.ResponseWriter, r *http.Request) {
	var p struct {
		Ticket string `json:"ticket"`
	}
	if !decodeDecisionJSON(w, r, &p, 2048) {
		return
	}
	role, ok := s.decisions.lookup(p.Ticket, true)
	if !ok {
		uxlog.Log("[decide] exchange rejected")
		writeErr(w, 401, "decision link expired", nil)
		return
	}
	session, expires, err := s.decisions.mint(role, true)
	if err != nil {
		writeErr(w, 503, "decision session unavailable", nil)
		return
	}
	uxlog.Log("[decide] exchanged role=%d", role)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, 200, map[string]any{"session": session, "initial_role_id": initialDecisionRole(role), "expires_at": expires})
}
func (s *Server) handleDecisions(w http.ResponseWriter, r *http.Request) {
	role, ok := s.decisionRole(w, r)
	if !ok {
		return
	}
	items, err := s.openDecisions(role)
	if err != nil {
		uxlog.Log("[decide] read failed role=%d", role)
		writeErr(w, 500, "decisions unavailable", nil)
		return
	}
	uxlog.Log("[decide] read role=%d count=%d", role, len(items))
	w.Header().Set("Cache-Control", "no-store")
	coordinator, err := s.db.HeraRole(role)
	if err != nil {
		writeErr(w, 500, "board unavailable", nil)
		return
	}
	writeJSON(w, 200, map[string]any{"role_id": role, "coordinator": coordinator.Name, "items": items})
}
func (s *Server) handleDecisionAnswers(w http.ResponseWriter, r *http.Request) {
	role, ok := s.decisionRole(w, r)
	if !ok {
		return
	}
	var p struct {
		Answers []db.HeraBoardAnswer `json:"answers"`
	}
	if !decodeDecisionJSON(w, r, &p, boardResponseBodyMaxBytes) {
		return
	}
	valid := len(p.Answers) > 0 && len(p.Answers) <= 16
	seen := map[int64]bool{}
	ids := map[string]bool{}
	for _, a := range p.Answers {
		durable := a.ActionID != "" || a.Action == "comment" || len(a.Annotations) > 0
		if seen[a.ItemID] || a.ItemID <= 0 || a.ExpectedUpdatedAt.IsZero() || len(a.Note) > 4096 || !db.ValidBoardAnnotationEnvelope(a.Annotations) || (durable && (!fyiActionID.MatchString(a.ActionID) || ids[a.ActionID])) {
			valid = false
		}
		seen[a.ItemID] = true
		if durable {
			ids[a.ActionID] = true
		}
	}
	if !valid {
		uxlog.Log("[decide] invalid answer envelope role=%d", role)
		writeJSON(w, 400, map[string]string{"code": "invalid_request", "error": "answers require 1-16 unique items, revisions, notes up to 4 KiB, and at most 20 valid annotations; feedback requires a unique action_id"})
		return
	}
	results, err := s.db.AnswerHeraBoardItems(role, p.Answers)
	if err != nil {
		uxlog.Log("[decide] answers failed role=%d", role)
		writeErr(w, 500, "answers unavailable", nil)
		return
	}
	count, _, err := s.db.HeraBoardUnread(role)
	if err != nil {
		writeErr(w, 500, "board unavailable", nil)
		return
	}
	uxlog.Log("[decide] answers role=%d count=%d", role, len(results))
	for _, result := range results {
		uxlog.Log("[decide] answer role=%d item=%d outcome=%s", role, result.ItemID, result.Outcome)
	}
	writeJSON(w, 200, map[string]any{"results": results, "unread_count": count})
}
