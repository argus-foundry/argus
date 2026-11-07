package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drn/argus/internal/agent"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

type fakeTermSession struct {
	agent.SessionHandle
	mu      sync.Mutex
	ring    []byte
	ringCap int
	total   uint64
	writers []io.Writer
	done    chan struct{}
}

func newFakeTermSession(ringCap int) *fakeTermSession {
	return &fakeTermSession{ringCap: ringCap, done: make(chan struct{})}
}

func (f *fakeTermSession) emit(p []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ring = append(f.ring, p...)
	if len(f.ring) > f.ringCap {
		f.ring = f.ring[len(f.ring)-f.ringCap:]
	}
	f.total += uint64(len(p))
	for _, w := range f.writers {
		_, _ = w.Write(p)
	}
}

func (f *fakeTermSession) AddWriterFromAt(w io.Writer, offset uint64) (uint64, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	start := f.total
	if offset < f.total {
		gap := f.total - offset
		replay := f.ring
		if gap <= uint64(len(f.ring)) {
			replay = f.ring[len(f.ring)-int(gap):] //nolint:gosec // gap <= len(ring)
		}
		start = f.total - uint64(len(replay))
		_, _ = w.Write(append([]byte(nil), replay...))
	}
	f.writers = append(f.writers, w)
	return start, true
}

func (f *fakeTermSession) RemoveWriter(w io.Writer) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, x := range f.writers {
		if x == w {
			f.writers = append(f.writers[:i], f.writers[i+1:]...)
			return
		}
	}
}

func (f *fakeTermSession) writerCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.writers)
}

func (f *fakeTermSession) TotalWritten() uint64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.total
}
func (f *fakeTermSession) Done() <-chan struct{} { return f.done }

type fakeTermRunner struct {
	agent.SessionRunner
	mu       sync.Mutex
	sessions map[string]agent.SessionHandle
}

func (r *fakeTermRunner) Get(id string) agent.SessionHandle {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.sessions[id]; ok {
		return s
	}
	return nil
}

func (r *fakeTermRunner) set(id string, s agent.SessionHandle) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessions[id] = s
}

func termTestServer(t *testing.T) (*Server, *fakeTermRunner, *httptest.Server) {
	t.Helper()
	srv, d := testServer(t)
	for _, id := range []string{"ta", "tb", "tc"} {
		testutil.NoError(t, d.Add(&model.Task{ID: id, Name: id, Status: model.StatusInProgress}))
	}
	fr := &fakeTermRunner{SessionRunner: srv.runner, sessions: map[string]agent.SessionHandle{}}
	srv.runner = fr
	oldSync := termSyncInterval
	termSyncInterval = 20 * time.Millisecond
	t.Cleanup(func() { termSyncInterval = oldSync })
	ts := httptest.NewServer(authMiddleware(srv.token, srv.db, nil, srv.routes()))
	t.Cleanup(ts.Close)
	return srv, fr, ts
}

func termDo(t *testing.T, ts *httptest.Server, method, path, token, body string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(method, ts.URL+path, strings.NewReader(body))
	testutil.NoError(t, err)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	testutil.NoError(t, err)
	return resp
}

func termCreate(t *testing.T, ts *httptest.Server, body string) (id, url string) {
	t.Helper()
	resp := termDo(t, ts, "POST", "/api/terminal-streams", "test-token", body)
	defer resp.Body.Close()
	testutil.Equal(t, resp.StatusCode, http.StatusCreated)
	var out map[string]string
	testutil.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
	return out["stream_id"], out["url"]
}

type sseEvent struct {
	Name string
	Data map[string]any
}

type termClient struct {
	events chan sseEvent
	cancel context.CancelFunc
}

func termConnect(t *testing.T, ts *httptest.Server, url string) *termClient {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, "GET", ts.URL+url, nil)
	testutil.NoError(t, err)
	req.Header.Set("Authorization", "Bearer test-token")
	resp, err := http.DefaultClient.Do(req)
	testutil.NoError(t, err)
	testutil.Equal(t, resp.StatusCode, http.StatusOK)
	c := &termClient{events: make(chan sseEvent, 256), cancel: cancel}
	go func() {
		defer resp.Body.Close()
		sc := bufio.NewScanner(resp.Body)
		sc.Buffer(make([]byte, 1<<20), 4<<20)
		var name string
		for sc.Scan() {
			line := sc.Text()
			switch {
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				var m map[string]any
				if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &m) == nil {
					c.events <- sseEvent{Name: name, Data: m}
				}
			}
		}
		close(c.events)
	}()
	t.Cleanup(cancel)
	return c
}

func (c *termClient) next(t *testing.T) sseEvent {
	t.Helper()
	select {
	case ev, ok := <-c.events:
		if !ok {
			t.Fatal("stream closed")
		}
		return ev
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for event")
	}
	return sseEvent{}
}

func (c *termClient) nextFor(t *testing.T, task string) sseEvent {
	t.Helper()
	for {
		ev := c.next(t)
		if ev.Data["task_id"] == task {
			return ev
		}
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met")
}

func outputBytes(t *testing.T, ev sseEvent) []byte {
	t.Helper()
	b, err := base64.StdEncoding.DecodeString(ev.Data["data_base64"].(string))
	testutil.NoError(t, err)
	return b
}

func TestTermStream_CreateAndErrors(t *testing.T) {
	_, _, ts := termTestServer(t)

	t.Run("shape", func(t *testing.T) {
		id, url := termCreate(t, ts, `{"subscriptions":[{"task_id":"ta","session_id":"s","since":1000}]}`)
		testutil.True(t, id != "")
		testutil.Equal(t, url, "/api/terminal-streams/"+id+"/events")
	})
	t.Run("missing auth", func(t *testing.T) {
		resp := termDo(t, ts, "POST", "/api/terminal-streams", "", `{"subscriptions":[]}`)
		defer resp.Body.Close()
		testutil.Equal(t, resp.StatusCode, http.StatusUnauthorized)
	})
	t.Run("invalid", func(t *testing.T) {
		for name, body := range map[string]string{
			"empty task id": `{"subscriptions":[{"task_id":""}]}`,
			"duplicate":     `{"subscriptions":[{"task_id":"ta"},{"task_id":"ta"}]}`,
			"negative":      `{"subscriptions":[{"task_id":"ta","since":-1}]}`,
			"not json":      `nope`,
			"unknown field": `{"subscriptions":[],"x":1}`,
		} {
			resp := termDo(t, ts, "POST", "/api/terminal-streams", "test-token", body)
			resp.Body.Close()
			testutil.Equal(t, resp.StatusCode, http.StatusBadRequest)
			_ = name
		}
	})
	t.Run("over 64", func(t *testing.T) {
		var subs []string
		for i := 0; i < 65; i++ {
			subs = append(subs, fmt.Sprintf(`{"task_id":"t%d"}`, i))
		}
		resp := termDo(t, ts, "POST", "/api/terminal-streams", "test-token", `{"subscriptions":[`+strings.Join(subs, ",")+`]}`)
		resp.Body.Close()
		testutil.Equal(t, resp.StatusCode, http.StatusRequestEntityTooLarge)
	})
	t.Run("unknown stream", func(t *testing.T) {
		for _, m := range []string{"PUT", "DELETE", "GET"} {
			p := "/api/terminal-streams/nope"
			if m == "GET" {
				p += "/events"
			}
			resp := termDo(t, ts, m, p, "test-token", `{"subscriptions":[]}`)
			resp.Body.Close()
			testutil.Equal(t, resp.StatusCode, http.StatusNotFound)
		}
	})
}

func TestTermStream_WrongPrincipal(t *testing.T) {
	srv, _, ts := termTestServer(t)
	dev, _, err := MintToken(srv.db, "other")
	testutil.NoError(t, err)
	id, url := termCreate(t, ts, `{"subscriptions":[]}`)
	for _, c := range []struct{ m, p string }{{"GET", url}, {"PUT", "/api/terminal-streams/" + id}, {"DELETE", "/api/terminal-streams/" + id}} {
		resp := termDo(t, ts, c.m, c.p, dev, `{"subscriptions":[]}`)
		resp.Body.Close()
		testutil.Equal(t, resp.StatusCode, http.StatusForbidden)
	}
	resp := termDo(t, ts, "DELETE", "/api/terminal-streams/"+id, "test-token", "")
	resp.Body.Close()
	testutil.Equal(t, resp.StatusCode, http.StatusNoContent)
}

func TestTermStream_MultiplexContiguousAndReconnect(t *testing.T) {
	_, fr, ts := termTestServer(t)
	a, b := newFakeTermSession(1024), newFakeTermSession(1024)
	fr.set("ta", a)
	fr.set("tb", b)
	a.emit([]byte("a-one"))

	_, url := termCreate(t, ts, `{"subscriptions":[{"task_id":"ta"},{"task_id":"tb"}]}`)
	c := termConnect(t, ts, url)

	ev := c.nextFor(t, "ta")
	testutil.Equal(t, ev.Name, "output")
	testutil.Equal(t, string(outputBytes(t, ev)), "a-one")
	testutil.Equal(t, ev.Data["from"].(float64), float64(0))
	testutil.Equal(t, ev.Data["to"].(float64), float64(5))
	sessA := ev.Data["session_id"].(string)

	b.emit([]byte("b-one"))
	a.emit([]byte{0xff, 0x00, 0xfe})
	got := map[string]sseEvent{}
	for len(got) < 2 {
		e := c.next(t)
		got[e.Data["task_id"].(string)] = e
	}
	evb, eva := got["tb"], got["ta"]
	testutil.Equal(t, string(outputBytes(t, evb)), "b-one")
	testutil.Equal(t, evb.Data["from"].(float64), float64(0))
	testutil.DeepEqual(t, outputBytes(t, eva), []byte{0xff, 0x00, 0xfe})
	testutil.Equal(t, eva.Data["from"].(float64), float64(5))
	testutil.Equal(t, eva.Data["to"].(float64), float64(8))

	c.cancel()
	waitFor(t, func() bool { return a.writerCount() == 0 && b.writerCount() == 0 })
	a.emit([]byte("missed"))

	_, url2 := termCreate(t, ts, fmt.Sprintf(`{"subscriptions":[{"task_id":"ta","session_id":%q,"since":8}]}`, sessA))
	c2 := termConnect(t, ts, url2)
	ev2 := c2.nextFor(t, "ta")
	testutil.Equal(t, ev2.Name, "output")
	testutil.Equal(t, string(outputBytes(t, ev2)), "missed")
	testutil.Equal(t, ev2.Data["from"].(float64), float64(8))
	testutil.Equal(t, ev2.Data["to"].(float64), float64(14))
}

func TestTermStream_GapOnEviction(t *testing.T) {
	_, fr, ts := termTestServer(t)
	a := newFakeTermSession(8)
	fr.set("ta", a)
	a.emit([]byte("0123456789abcdef"))

	_, url := termCreate(t, ts, `{"subscriptions":[{"task_id":"ta","since":2}]}`)
	c := termConnect(t, ts, url)
	gap := c.next(t)
	testutil.Equal(t, gap.Name, "gap")
	testutil.Equal(t, gap.Data["requested"].(float64), float64(2))
	testutil.Equal(t, gap.Data["oldest"].(float64), float64(8))
	testutil.Equal(t, gap.Data["current"].(float64), float64(16))
	out := c.next(t)
	testutil.Equal(t, out.Name, "output")
	testutil.Equal(t, out.Data["from"].(float64), float64(8))
	testutil.Equal(t, string(outputBytes(t, out)), "89abcdef")
}

func TestTermStream_ResetOnSessionIdentity(t *testing.T) {
	_, fr, ts := termTestServer(t)
	a := newFakeTermSession(64)
	fr.set("ta", a)
	a.emit([]byte("old"))

	t.Run("stale session id at subscribe", func(t *testing.T) {
		_, url := termCreate(t, ts, `{"subscriptions":[{"task_id":"ta","session_id":"stale","since":3}]}`)
		c := termConnect(t, ts, url)
		reset := c.next(t)
		testutil.Equal(t, reset.Name, "reset")
		out := c.next(t)
		testutil.Equal(t, out.Name, "output")
		testutil.Equal(t, out.Data["session_id"], reset.Data["session_id"])
		testutil.Equal(t, string(outputBytes(t, out)), "old")
		testutil.Equal(t, out.Data["from"].(float64), float64(0))
	})

	t.Run("replacement while connected", func(t *testing.T) {
		_, url := termCreate(t, ts, `{"subscriptions":[{"task_id":"ta"}]}`)
		c := termConnect(t, ts, url)
		first := c.next(t)
		oldID := first.Data["session_id"].(string)
		b := newFakeTermSession(64)
		b.emit([]byte("new"))
		fr.set("ta", b)
		var reset sseEvent
		for {
			reset = c.next(t)
			if reset.Name == "reset" {
				break
			}
		}
		testutil.True(t, reset.Data["session_id"] != oldID)
		out := c.next(t)
		testutil.Equal(t, out.Name, "output")
		testutil.Equal(t, string(outputBytes(t, out)), "new")
		waitFor(t, func() bool { return a.writerCount() == 0 })
	})
}

func TestTermStream_PerSubscriptionErrorsAndExit(t *testing.T) {
	srv, fr, ts := termTestServer(t)
	a, b := newFakeTermSession(64), newFakeTermSession(64)
	fr.set("ta", a)
	fr.set("tb", b)
	testutil.NoError(t, srv.db.Add(&model.Task{ID: "tc2", Name: "tc2"}))

	_, url := termCreate(t, ts, `{"subscriptions":[{"task_id":"ta"},{"task_id":"tb"},{"task_id":"tc"},{"task_id":"ghost"}]}`)
	c := termConnect(t, ts, url)

	seen := map[string]string{}
	for len(seen) < 2 {
		ev := c.next(t)
		if ev.Name == "subscription_error" {
			seen[ev.Data["task_id"].(string)] = ev.Data["code"].(string)
		}
	}
	testutil.Equal(t, seen["tc"], "no_session")
	testutil.Equal(t, seen["ghost"], "no_task")

	close(a.done)
	ev := c.nextFor(t, "ta")
	testutil.Equal(t, ev.Name, "exit")

	b.emit([]byte("still-live"))
	out := c.nextFor(t, "tb")
	testutil.Equal(t, out.Name, "output")
	testutil.Equal(t, string(outputBytes(t, out)), "still-live")

	t.Run("late session appears as reset", func(t *testing.T) {
		cs := newFakeTermSession(64)
		cs.emit([]byte("late"))
		fr.set("tc", cs)
		ev := c.nextFor(t, "tc")
		testutil.Equal(t, ev.Name, "reset")
		out := c.nextFor(t, "tc")
		testutil.Equal(t, string(outputBytes(t, out)), "late")
	})
}

func TestTermStream_WriterCleanup(t *testing.T) {
	_, fr, ts := termTestServer(t)
	a, b := newFakeTermSession(64), newFakeTermSession(64)
	fr.set("ta", a)
	fr.set("tb", b)

	t.Run("delete", func(t *testing.T) {
		id, url := termCreate(t, ts, `{"subscriptions":[{"task_id":"ta"}]}`)
		c := termConnect(t, ts, url)
		waitFor(t, func() bool { return a.writerCount() == 1 })
		resp := termDo(t, ts, "DELETE", "/api/terminal-streams/"+id, "test-token", "")
		resp.Body.Close()
		testutil.Equal(t, resp.StatusCode, http.StatusNoContent)
		waitFor(t, func() bool { return a.writerCount() == 0 })
		_ = c
	})

	t.Run("put replaces atomically", func(t *testing.T) {
		id, url := termCreate(t, ts, `{"subscriptions":[{"task_id":"ta"}]}`)
		c := termConnect(t, ts, url)
		waitFor(t, func() bool { return a.writerCount() == 1 })
		resp := termDo(t, ts, "PUT", "/api/terminal-streams/"+id, "test-token", `{"subscriptions":[{"task_id":"tb"}]}`)
		var out map[string]string
		testutil.NoError(t, json.NewDecoder(resp.Body).Decode(&out))
		resp.Body.Close()
		testutil.Equal(t, resp.StatusCode, http.StatusOK)
		testutil.Equal(t, out["stream_id"], id)
		waitFor(t, func() bool { return a.writerCount() == 0 && b.writerCount() == 1 })
		b.emit([]byte("hi"))
		ev := c.nextFor(t, "tb")
		testutil.Equal(t, string(outputBytes(t, ev)), "hi")
		a.emit([]byte("ignored"))
		resp = termDo(t, ts, "DELETE", "/api/terminal-streams/"+id, "test-token", "")
		resp.Body.Close()
		waitFor(t, func() bool { return b.writerCount() == 0 })
	})

	t.Run("disconnect", func(t *testing.T) {
		id, url := termCreate(t, ts, `{"subscriptions":[{"task_id":"ta"}]}`)
		c := termConnect(t, ts, url)
		waitFor(t, func() bool { return a.writerCount() == 1 })
		c.cancel()
		waitFor(t, func() bool { return a.writerCount() == 0 })
		resp := termDo(t, ts, "PUT", "/api/terminal-streams/"+id, "test-token", `{"subscriptions":[]}`)
		resp.Body.Close()
		testutil.Equal(t, resp.StatusCode, http.StatusNotFound)
	})

	t.Run("second connection conflicts", func(t *testing.T) {
		id, url := termCreate(t, ts, `{"subscriptions":[]}`)
		termConnect(t, ts, url)
		waitFor(t, func() bool {
			resp := termDo(t, ts, "GET", url, "test-token", "")
			resp.Body.Close()
			return resp.StatusCode == http.StatusConflict
		})
		resp := termDo(t, ts, "DELETE", "/api/terminal-streams/"+id, "test-token", "")
		resp.Body.Close()
	})

	t.Run("expiry without connection", func(t *testing.T) {
		old := termStreamIdleTTL
		termStreamIdleTTL = 30 * time.Millisecond
		defer func() { termStreamIdleTTL = old }()
		_, url := termCreate(t, ts, `{"subscriptions":[{"task_id":"ta"}]}`)
		time.Sleep(150 * time.Millisecond)
		resp := termDo(t, ts, "GET", url, "test-token", "")
		resp.Body.Close()
		testutil.Equal(t, resp.StatusCode, http.StatusNotFound)
		testutil.Equal(t, a.writerCount(), 0)
	})
}

type nopFlusher struct{}

func (nopFlusher) Flush() {}

func TestTermStream_SlowClientOverflowResyncs(t *testing.T) {
	srv, fr, _ := termTestServer(t)
	a := newFakeTermSession(64 << 10)
	fr.set("ta", a)
	st := &termStream{
		srv: srv, id: "x", wake: make(chan struct{}, 1), done: make(chan struct{}),
		specs: []termSubReq{{TaskID: "ta"}},
	}
	st.mu.Lock()
	st.subs = st.buildSubsLocked()
	st.mu.Unlock()
	st.attachAll()

	chunk := bytes.Repeat([]byte("x"), 64<<10)
	finished := make(chan struct{})
	go func() {
		for i := 0; i < 24; i++ {
			a.emit(chunk)
		}
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(3 * time.Second):
		t.Fatal("producer stalled by an unread subscription")
	}
	sub := st.subs[0]
	sub.mu.Lock()
	overflowed, queued := sub.overflow, sub.queued
	sub.mu.Unlock()
	testutil.True(t, overflowed)
	testutil.Equal(t, queued, 0)

	var buf bytes.Buffer
	st.drain(&buf, nopFlusher{})
	out := buf.String()
	testutil.True(t, strings.Contains(out, "event: gap"))
	testutil.True(t, strings.Contains(out, "event: output"))
	testutil.Equal(t, a.writerCount(), 1)
	st.close()
	testutil.Equal(t, a.writerCount(), 0)
}
