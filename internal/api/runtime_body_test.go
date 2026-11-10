package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestRuntimeStalledBodyDoesNotBlockTask(t *testing.T) {
	srv, d := testServer(t)
	task := addStopped(t, d, nil)
	body := &stalledGuardBody{entered: make(chan struct{}), release: make(chan struct{})}
	req := authedReq("PATCH", "/api/tasks/"+task.ID+"/runtime", "")
	req.Body = body
	done := make(chan struct{})
	go func() { defer close(done); srv.routes().ServeHTTP(httptest.NewRecorder(), req) }()
	<-body.entered
	archive := authedReq("POST", "/api/tasks/"+task.ID+"/archive", "")
	w := httptest.NewRecorder()
	archived := make(chan struct{})
	go func() { defer close(archived); srv.routes().ServeHTTP(w, archive) }()
	completed := false
	select {
	case <-archived:
		completed = true
	case <-time.After(time.Second):
	}
	close(body.release)
	<-done
	<-archived
	testutil.True(t, completed)

	testutil.Equal(t, w.Code, http.StatusOK)
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.True(t, got.Archived)
}

func TestRuntimeCanceledRequestDoesNotWrite(t *testing.T) {
	srv, d := testServer(t)
	task := addStopped(t, d, nil)
	_, rev := runtimeGet(t, srv, task.ID)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := authedReq("PATCH", "/api/tasks/"+task.ID+"/runtime", `{"model":"opus","apply":"next_resume"}`).WithContext(ctx)
	req.Header.Set("If-Match", rev)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	testutil.Equal(t, w.Code, http.StatusServiceUnavailable)
	got, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, got.Model, "")
}

func TestRuntimeRejectsWholeInvalidBody(t *testing.T) {
	for _, suffix := range []string{" garbage", " {}", strings.Repeat(" ", 256*1024), strings.Repeat("x", 300000)} {
		t.Run(strings.TrimSpace(suffix[:min(len(suffix), 12)]), func(t *testing.T) {
			srv, d := testServer(t)
			task := addStopped(t, d, nil)
			_, rev := runtimeGet(t, srv, task.ID)
			sink := installAPISink(t)
			w := runtimePatch(srv, task.ID, `{"model":"opus","apply":"next_resume"}`+suffix, ifMatch(rev))
			testutil.Equal(t, w.Code, http.StatusBadRequest)
			testutil.Equal(t, decodeBody(t, w)["code"], any("invalid_body"))
			got, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, got.Model, "")
			testutil.Equal(t, len(runtimeChangedEvents(sink)), 0)
		})
	}
}

func TestRuntimeRejectsNUL(t *testing.T) {
	for _, body := range []string{`{"prompt":"hello\u0000world","apply":"next_resume"}`, `{"prompt":"hello\u0000world","apply":"restart"}`, `{"model":"opus\u0000x","apply":"next_resume"}`} {
		t.Run(body, func(t *testing.T) {
			srv, d := testServer(t)
			task := addStopped(t, d, nil)
			_, rev := runtimeGet(t, srv, task.ID)
			runner := &guardRunner{SessionRunner: srv.runner, startErr: http.ErrAbortHandler}
			srv.runner = runner
			sink := installAPISink(t)
			w := runtimePatch(srv, task.ID, body, ifMatch(rev))
			testutil.Equal(t, w.Code, http.StatusBadRequest)
			testutil.Equal(t, decodeBody(t, w)["code"], any("invalid_runtime"))
			got, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, got.Prompt, task.Prompt)
			testutil.Equal(t, got.Model, task.Model)
			testutil.Equal(t, runner.calls, 0)
			testutil.Equal(t, len(runtimeChangedEvents(sink)), 0)
		})
	}
}
