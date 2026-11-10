package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

type stalledAccountBody struct {
	entered, release chan struct{}
	reader           io.Reader
	once             sync.Once
}

func (b *stalledAccountBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered); <-b.release })
	return b.reader.Read(p)
}
func (b *stalledAccountBody) Close() error { return nil }

func TestAccountWrite_BodyDoesNotBlockOtherWrites(t *testing.T) {
	for _, route := range []struct{ method, path, body string }{
		{"POST", "/api/accounts", `{"name":"new","claude_config_dir":"/tmp/account-new"}`},
		{"PATCH", "/api/accounts/work", `{"label":"changed"}`},
		{"PUT", "/api/account-defaults", `{"account":null}`},
		{"PUT", "/api/projects/argus/account", `{"account":null}`},
	} {
		t.Run(route.method+route.path, func(t *testing.T) {
			srv := defaultsServer(t, "")
			body := &stalledAccountBody{entered: make(chan struct{}), release: make(chan struct{}), reader: strings.NewReader(route.body)}
			var release sync.Once
			unblock := func() { release.Do(func() { close(body.release) }) }
			defer unblock()
			req := acctReq(route.method, route.path, "", "master")
			req.Body = body
			slow := make(chan int, 1)
			go func() { w := httptest.NewRecorder(); srv.routes().ServeHTTP(w, req); slow <- w.Code }()
			select {
			case <-body.entered:
			case <-time.After(5 * time.Second):
				t.Fatal("body read did not start")
			}
			other := make(chan int, 1)
			go func() {
				other <- serveAccountRequest(srv, acctReq("PATCH", "/api/accounts/personal", `{"label":"independent"}`, "master")).Code
			}()
			blocked := false
			select {
			case code := <-other:
				testutil.Equal(t, code, http.StatusOK)
			case <-time.After(2 * time.Second):
				blocked = true
				t.Error("unrelated write blocked by stalled body")
			}
			unblock()
			select {
			case code := <-slow:
				testutil.True(t, code == http.StatusOK || code == http.StatusCreated)
			case <-time.After(5 * time.Second):
				t.Fatal("body handler did not finish")
			}
			if blocked {
				select {
				case code := <-other:
					testutil.Equal(t, code, http.StatusOK)
				case <-time.After(5 * time.Second):
					t.Fatal("other write did not finish")
				}
			}
		})
	}
}
