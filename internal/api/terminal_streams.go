package api

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/uxlog"
)

const (
	maxTermSubscriptions = 64
	maxTermStreams       = 256
	termSubQueueLimit    = 1 << 20
	termFrameMax         = 64 << 10
	termKeepalive        = 30 * time.Second
	termMaxIDLen         = 256
)

var (
	termStreamIdleTTL = 60 * time.Second
	termSyncInterval  = time.Second
)

type termSubReq struct {
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id"`
	Since     uint64 `json:"since"`
}

type termStreamReq struct {
	Subscriptions []termSubReq `json:"subscriptions"`
}

type termItem struct {
	event   string
	payload map[string]any
	rel     uint64
	data    []byte
}

// termSub is one task subscription on a multiplexed stream. All mutable
// fields are guarded by mu; the writer registered on the session only ever
// appends to items under mu and never blocks.
type termSub struct {
	taskID string
	wake   func()

	mu       sync.Mutex
	items    []termItem
	queued   int
	next     uint64
	sess     agent.SessionHandle
	sessID   string
	writer   *termWriter
	ready    bool
	overflow bool
	errCode  string
}

type termWriter struct {
	sub  *termSub
	stop chan struct{}
	n    uint64
	base uint64
	dead bool
}

func (w *termWriter) Write(p []byte) (int, error) {
	sub := w.sub
	sub.mu.Lock()
	defer sub.mu.Unlock()
	if w.dead || sub.writer != w {
		return len(p), nil
	}
	rel := w.n
	w.n += uint64(len(p))
	if sub.overflow {
		return len(p), nil
	}
	if sub.ready && sub.queued+len(p) > termSubQueueLimit {
		sub.overflow = true
		sub.items = nil
		sub.queued = 0
		sub.wake()
		return len(p), nil
	}
	if n := len(sub.items); n > 0 {
		last := &sub.items[n-1]
		if last.event == "output" && last.rel+uint64(len(last.data)) == rel && len(last.data)+len(p) <= termFrameMax {
			last.data = append(last.data, p...)
			sub.queued += len(p)
			sub.wake()
			return len(p), nil
		}
	}
	cp := make([]byte, len(p))
	copy(cp, p)
	sub.items = append(sub.items, termItem{event: "output", rel: rel, data: cp})
	sub.queued += len(p)
	sub.wake()
	return len(p), nil
}

type termStream struct {
	srv       *Server
	id        string
	principal string

	mu        sync.Mutex
	specs     []termSubReq
	subs      []*termSub
	connected bool
	closed    bool
	expire    *time.Timer
	wake      chan struct{}
	done      chan struct{}
}

func (st *termStream) signal() {
	select {
	case st.wake <- struct{}{}:
	default:
	}
}

type termIdent struct {
	sess agent.SessionHandle
	id   string
}

func (s *Server) termSessionID(taskID string, sess agent.SessionHandle) string {
	s.termMu.Lock()
	defer s.termMu.Unlock()
	if s.termIdents == nil {
		s.termIdents = map[string]termIdent{}
	}
	if cur, ok := s.termIdents[taskID]; ok && cur.sess == sess {
		return cur.id
	}
	var b [8]byte
	_, _ = rand.Read(b[:])
	id := hex.EncodeToString(b[:])
	s.termIdents[taskID] = termIdent{sess: sess, id: id}
	return id
}

func termPrincipal(r *http.Request) string {
	return authOrigin(r) + ":" + r.Header.Get("X-Argus-Token-Id")
}

func validateTermSubs(w http.ResponseWriter, r *http.Request) ([]termSubReq, bool) {
	var req termStreamReq
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid request body", err)
		return nil, false
	}
	if len(req.Subscriptions) > maxTermSubscriptions {
		writeErr(w, http.StatusRequestEntityTooLarge, fmt.Sprintf("at most %d subscriptions", maxTermSubscriptions), nil)
		return nil, false
	}
	seen := make(map[string]bool, len(req.Subscriptions))
	for _, sub := range req.Subscriptions {
		if sub.TaskID == "" || len(sub.TaskID) > termMaxIDLen || len(sub.SessionID) > termMaxIDLen {
			writeErr(w, http.StatusBadRequest, "invalid task_id or session_id", nil)
			return nil, false
		}
		if seen[sub.TaskID] {
			writeErr(w, http.StatusBadRequest, "duplicate task_id "+sub.TaskID, nil)
			return nil, false
		}
		seen[sub.TaskID] = true
	}
	return req.Subscriptions, true
}

func (s *Server) handleTermStreamCreate(w http.ResponseWriter, r *http.Request) {
	subs, ok := validateTermSubs(w, r)
	if !ok {
		return
	}
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		writeErr(w, http.StatusInternalServerError, "id generation failed", err)
		return
	}
	st := &termStream{
		srv:       s,
		id:        hex.EncodeToString(b[:]),
		principal: termPrincipal(r),
		specs:     subs,
		wake:      make(chan struct{}, 1),
		done:      make(chan struct{}),
	}
	s.termMu.Lock()
	if s.termStreams == nil {
		s.termStreams = map[string]*termStream{}
	}
	if len(s.termStreams) >= maxTermStreams {
		s.termMu.Unlock()
		writeErr(w, http.StatusTooManyRequests, "too many terminal streams", nil)
		return
	}
	s.termStreams[st.id] = st
	s.termMu.Unlock()
	st.mu.Lock()
	st.expire = time.AfterFunc(termStreamIdleTTL, func() {
		uxlog.Log("[termstream] expired unused stream=%s", st.id)
		st.close()
	})
	st.mu.Unlock()
	uxlog.Log("[termstream] created stream=%s subs=%d", st.id, len(subs))
	writeJSON(w, http.StatusCreated, map[string]string{
		"stream_id": st.id,
		"url":       "/api/terminal-streams/" + st.id + "/events",
	})
}

// lookupTermStream resolves the {id} path value, writing 404/403 itself.
func (s *Server) lookupTermStream(w http.ResponseWriter, r *http.Request) *termStream {
	s.termMu.Lock()
	st := s.termStreams[r.PathValue("id")]
	s.termMu.Unlock()
	if st == nil {
		writeErr(w, http.StatusNotFound, "terminal stream not found or expired", nil)
		return nil
	}
	if st.principal != termPrincipal(r) {
		writeErr(w, http.StatusForbidden, "terminal stream belongs to another principal", nil)
		return nil
	}
	return st
}

func (s *Server) handleTermStreamReplace(w http.ResponseWriter, r *http.Request) {
	st := s.lookupTermStream(w, r)
	if st == nil {
		return
	}
	subs, ok := validateTermSubs(w, r)
	if !ok {
		return
	}
	st.mu.Lock()
	if st.closed {
		st.mu.Unlock()
		writeErr(w, http.StatusNotFound, "terminal stream not found or expired", nil)
		return
	}
	st.specs = subs
	if st.connected {
		old := st.subs
		st.subs = st.buildSubsLocked()
		st.mu.Unlock()
		for _, sub := range old {
			sub.detach()
		}
		st.attachAll()
	} else {
		st.mu.Unlock()
	}
	uxlog.Log("[termstream] replaced stream=%s subs=%d", st.id, len(subs))
	writeJSON(w, http.StatusOK, map[string]string{"stream_id": st.id})
}

func (s *Server) handleTermStreamDelete(w http.ResponseWriter, r *http.Request) {
	st := s.lookupTermStream(w, r)
	if st == nil {
		return
	}
	st.close()
	uxlog.Log("[termstream] deleted stream=%s", st.id)
	w.WriteHeader(http.StatusNoContent)
}

func (st *termStream) buildSubsLocked() []*termSub {
	subs := make([]*termSub, len(st.specs))
	for i, spec := range st.specs {
		subs[i] = &termSub{taskID: spec.TaskID, wake: st.signal, next: spec.Since, sessID: spec.SessionID}
	}
	return subs
}

// attachAll registers a writer for each current subscription using its spec
// cursor. Called without st.mu held because attaching runs session replay.
func (st *termStream) attachAll() {
	st.mu.Lock()
	subs := append([]*termSub(nil), st.subs...)
	specs := append([]termSubReq(nil), st.specs...)
	st.mu.Unlock()
	for i, sub := range subs {
		st.attach(sub, specs[i].Since, specs[i].SessionID, false)
	}
	st.signal()
}

func (sub *termSub) detach() {
	sub.mu.Lock()
	w, sess := sub.writer, sub.sess
	sub.writer = nil
	sub.items = nil
	sub.queued = 0
	if w != nil {
		w.dead = true
		close(w.stop)
	}
	sub.mu.Unlock()
	if w != nil && sess != nil {
		sess.RemoveWriter(w)
	}
}

func (sub *termSub) pushEvent(event string, payload map[string]any) {
	sub.items = append(sub.items, termItem{event: event, payload: payload})
}

// attach (re)registers sub's writer at since. A requested session id that no
// longer matches the live one, or forceReset, emits reset and replays the new
// session from its start. Must not be called with sub.mu or st.mu held.
func (st *termStream) attach(sub *termSub, since uint64, reqSessID string, forceReset bool) {
	sub.detach()
	sess := st.srv.runner.Get(sub.taskID)
	if sess == nil {
		code := "no_session"
		if t, err := st.srv.db.Get(sub.taskID); err != nil || t == nil {
			code = "no_task"
		}
		sub.mu.Lock()
		sub.sess = nil
		if sub.errCode != code {
			sub.errCode = code
			sub.pushEvent("subscription_error", map[string]any{"task_id": sub.taskID, "code": code})
		}
		sub.ready = true
		sub.mu.Unlock()
		return
	}
	adder, ok := sess.(agent.OffsetWriterAdder)
	if !ok {
		sub.mu.Lock()
		sub.sess = nil
		if sub.errCode != "unsupported" {
			sub.errCode = "unsupported"
			sub.pushEvent("subscription_error", map[string]any{"task_id": sub.taskID, "code": "unsupported"})
		}
		sub.ready = true
		sub.mu.Unlock()
		return
	}
	id := st.srv.termSessionID(sub.taskID, sess)
	var prefix []termItem
	if forceReset || (reqSessID != "" && reqSessID != id) {
		prefix = append(prefix, termItem{event: "reset", payload: map[string]any{"task_id": sub.taskID, "session_id": id}})
		since = 0
	}
	w := &termWriter{sub: sub, stop: make(chan struct{})}
	sub.mu.Lock()
	sub.writer = w
	sub.sess = sess
	sub.sessID = id
	sub.errCode = ""
	sub.ready = false
	sub.overflow = false
	sub.items = nil
	sub.queued = 0
	sub.mu.Unlock()

	start, _ := adder.AddWriterFromAt(w, since)

	sub.mu.Lock()
	w.base = start
	sub.next = start
	if start != since {
		prefix = append(prefix, termItem{event: "gap", payload: map[string]any{
			"task_id": sub.taskID, "session_id": id,
			"requested": since, "oldest": start, "current": sess.TotalWritten(),
		}})
	}
	sub.items = append(prefix, sub.items...)
	sub.ready = true
	sub.mu.Unlock()

	go func() {
		select {
		case <-sess.Done():
			sub.mu.Lock()
			if sub.writer == w {
				sub.pushEvent("exit", map[string]any{"task_id": sub.taskID, "session_id": id})
				sub.wake()
			}
			sub.mu.Unlock()
		case <-w.stop:
		}
	}()
}

// sync re-evaluates each subscription against the runner so session
// replacement and late-appearing sessions surface as reset events.
func (st *termStream) sync() {
	st.mu.Lock()
	subs := append([]*termSub(nil), st.subs...)
	st.mu.Unlock()
	for _, sub := range subs {
		sess := st.srv.runner.Get(sub.taskID)
		if sess == nil {
			continue
		}
		id := st.srv.termSessionID(sub.taskID, sess)
		sub.mu.Lock()
		changed := (id != sub.sessID || sub.sess == nil) && sub.errCode != "unsupported"
		sub.mu.Unlock()
		if changed {
			uxlog.Log("[termstream] stream=%s task=%s session changed to %s", st.id, sub.taskID, id)
			st.attach(sub, 0, "", true)
			st.signal()
		}
	}
}

// pop returns the next frame for sub, or reports that an overflow needs a
// re-attach. ok is false when nothing is ready.
func (sub *termSub) pop() (event string, payload map[string]any, overflow, ok bool) {
	sub.mu.Lock()
	defer sub.mu.Unlock()
	if sub.overflow {
		return "", nil, true, true
	}
	if !sub.ready || len(sub.items) == 0 {
		return "", nil, false, false
	}
	it := &sub.items[0]
	if it.event != "output" {
		ev, p := it.event, it.payload
		sub.items = sub.items[1:]
		return ev, p, false, true
	}
	n := len(it.data)
	if n > termFrameMax {
		n = termFrameMax
	}
	chunk := it.data[:n]
	from := sub.writer.base + it.rel
	if n == len(it.data) {
		sub.items = sub.items[1:]
	} else {
		it.data = it.data[n:]
		it.rel += uint64(n)
	}
	sub.queued -= n
	sub.next = from + uint64(n)
	return "output", map[string]any{
		"task_id": sub.taskID, "session_id": sub.sessID,
		"from": from, "to": from + uint64(n),
		"data_base64": base64.StdEncoding.EncodeToString(chunk),
	}, false, true
}

func (st *termStream) close() {
	st.mu.Lock()
	if st.closed {
		st.mu.Unlock()
		return
	}
	st.closed = true
	subs := st.subs
	st.subs = nil
	if st.expire != nil {
		st.expire.Stop()
	}
	close(st.done)
	st.mu.Unlock()
	for _, sub := range subs {
		sub.detach()
	}
	st.srv.termMu.Lock()
	delete(st.srv.termStreams, st.id)
	st.srv.termMu.Unlock()
}

func (s *Server) handleTermStreamEvents(w http.ResponseWriter, r *http.Request) {
	st := s.lookupTermStream(w, r)
	if st == nil {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, "streaming not supported", nil)
		return
	}
	st.mu.Lock()
	if st.closed {
		st.mu.Unlock()
		writeErr(w, http.StatusNotFound, "terminal stream not found or expired", nil)
		return
	}
	if st.connected {
		st.mu.Unlock()
		writeErr(w, http.StatusConflict, "terminal stream already connected", nil)
		return
	}
	st.connected = true
	if st.expire != nil {
		st.expire.Stop()
	}
	st.subs = st.buildSubsLocked()
	st.mu.Unlock()
	defer st.close()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	flusher.Flush()

	st.attachAll()
	uxlog.Log("[termstream] connected stream=%s", st.id)
	defer uxlog.Log("[termstream] closed stream=%s", st.id)

	keepalive := time.NewTicker(termKeepalive)
	defer keepalive.Stop()
	syncTick := time.NewTicker(termSyncInterval)
	defer syncTick.Stop()

	for {
		st.drain(w, flusher)
		select {
		case <-st.wake:
		case <-keepalive.C:
			fmt.Fprint(w, ": ping\n\n") //nolint:errcheck
			flusher.Flush()
		case <-syncTick.C:
			st.sync()
		case <-st.done:
			return
		case <-r.Context().Done():
			return
		}
	}
}

func (st *termStream) drain(w io.Writer, flusher http.Flusher) {
	for {
		st.mu.Lock()
		subs := append([]*termSub(nil), st.subs...)
		st.mu.Unlock()
		progressed := false
		for _, sub := range subs {
			event, payload, overflow, ok := sub.pop()
			if !ok {
				continue
			}
			progressed = true
			if overflow {
				sub.mu.Lock()
				next, sessID := sub.next, sub.sessID
				sub.mu.Unlock()
				uxlog.Log("[termstream] stream=%s task=%s slow client overflow, resyncing from %d", st.id, sub.taskID, next)
				st.attach(sub, next, sessID, false)
				continue
			}
			body, err := json.Marshal(payload)
			if err != nil {
				continue
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, body) //nolint:errcheck
		}
		if !progressed {
			return
		}
		flusher.Flush()
	}
}
