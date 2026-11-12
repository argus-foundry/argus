package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

type fakeRuntime struct{ snap RuntimeSnapshot }

func (f *fakeRuntime) Snapshot() RuntimeSnapshot { return f.snap }

func runtimeFixture(t *testing.T, snap RuntimeSnapshot) (*Server, http.Handler) {
	t.Helper()
	srv, d := testServer(t)
	srv.SetRuntimeSource(&fakeRuntime{snap})
	srv.restartDelay = 0
	srv.restartPreflight = func() error { return nil }
	return srv, authMiddleware(srv.token, d, nil, srv.routes())
}

func getRuntime(t *testing.T, h http.Handler) map[string]any {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq("GET", "/api/runtime", ""))
	testutil.Equal(t, w.Code, http.StatusOK)
	var out map[string]any
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
	return out
}

func TestRuntime_Identities(t *testing.T) {
	booted := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	t.Run("stale daemon, current supervisor", func(t *testing.T) {
		srv, h := runtimeFixture(t, RuntimeSnapshot{
			DaemonHash: "aa", DaemonBootedAt: booted, DaemonState: "stale", InstalledHash: "bb",
			SupervisorHash: "bb", SupervisorState: "current", SupervisorCompatibility: "compatible", RestartSupported: true,
		})
		out := getRuntime(t, h)
		testutil.DeepEqual(t, out, map[string]any{
			"boot_id":           srv.bootID,
			"daemon":            map[string]any{"hash": "aa", "booted_at": "2026-01-02T03:04:05Z", "state": "stale"},
			"installed":         map[string]any{"hash": "bb"},
			"supervisor":        map[string]any{"hash": "bb", "state": "current", "compatibility": "compatible"},
			"restart_supported": true,
		})
		testutil.Equal(t, out["boot_id"].(string), srv.bootID)
		d := out["daemon"].(map[string]any)
		testutil.Equal(t, d["state"].(string), "stale")
		testutil.Equal(t, d["hash"].(string), "aa")
		testutil.Equal(t, d["booted_at"].(string), "2026-01-02T03:04:05Z")
		testutil.Equal(t, out["installed"].(map[string]any)["hash"].(string), "bb")
		sup := out["supervisor"].(map[string]any)
		testutil.Equal(t, sup["state"].(string), "current")
		testutil.Equal(t, sup["compatibility"].(string), "compatible")
		testutil.Equal(t, out["restart_supported"].(bool), true)
	})
	t.Run("hash-stale supervisor stays compatible", func(t *testing.T) {
		_, h := runtimeFixture(t, RuntimeSnapshot{
			DaemonHash: "aa", DaemonState: "current", InstalledHash: "aa",
			SupervisorHash: "cc", SupervisorState: "stale", SupervisorCompatibility: "compatible", RestartSupported: true,
		})
		sup := getRuntime(t, h)["supervisor"].(map[string]any)
		testutil.Equal(t, sup["state"].(string), "stale")
		testutil.Equal(t, sup["compatibility"].(string), "compatible")
	})
	t.Run("missing identities are null and unknown", func(t *testing.T) {
		_, h := runtimeFixture(t, RuntimeSnapshot{})
		out := getRuntime(t, h)
		d := out["daemon"].(map[string]any)
		testutil.Equal(t, d["state"].(string), "unknown")
		testutil.Nil(t, d["hash"])
		testutil.Nil(t, d["booted_at"])
		testutil.Nil(t, out["installed"].(map[string]any)["hash"])
		sup := out["supervisor"].(map[string]any)
		testutil.Nil(t, sup["hash"])
		testutil.Equal(t, sup["state"].(string), "unknown")
		testutil.Equal(t, sup["compatibility"].(string), "unknown")
		testutil.Equal(t, out["restart_supported"].(bool), false)
	})
	t.Run("no source wired is all unknown", func(t *testing.T) {
		srv, d := testServer(t)
		h := authMiddleware(srv.token, d, nil, srv.routes())
		testutil.Equal(t, getRuntime(t, h)["restart_supported"].(bool), false)
	})
	t.Run("requires auth", func(t *testing.T) {
		_, h := runtimeFixture(t, RuntimeSnapshot{})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/api/runtime", nil))
		testutil.Equal(t, w.Code, http.StatusUnauthorized)
	})
	t.Run("device token may read", func(t *testing.T) {
		srv, h := runtimeFixture(t, RuntimeSnapshot{})
		plain, _, err := MintToken(srv.db, "phone")
		testutil.NoError(t, err)
		req := httptest.NewRequest("GET", "/api/runtime", nil)
		req.Header.Set("Authorization", "Bearer "+plain)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		testutil.Equal(t, w.Code, http.StatusOK)
	})
}

func postRestart(h http.Handler, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, authedReq("POST", "/api/daemon/restart", body))
	return w
}

func supported() RuntimeSnapshot { return RuntimeSnapshot{RestartSupported: true} }

func TestDaemonRestart_Responses(t *testing.T) {
	t.Run("accepted flushes before handoff and never builds", func(t *testing.T) {
		srv, h := runtimeFixture(t, supported())
		w := httptest.NewRecorder()
		type seen struct {
			flushed bool
			code    int
		}
		got := make(chan seen, 1)
		srv.restartSpawn = func() (func() error, error) {
			got <- seen{w.Flushed, w.Code}
			return func() error { return nil }, nil
		}
		h.ServeHTTP(w, authedReq("POST", "/api/daemon/restart", `{"expected_boot_id":"`+srv.bootID+`"}`))
		testutil.Equal(t, w.Code, http.StatusAccepted)
		testutil.Contains(t, w.Body.String(), `"restart_requested":true`)
		testutil.Contains(t, w.Body.String(), srv.bootID)
		select {
		case s := <-got:
			testutil.Equal(t, s.flushed, true)
			testutil.Equal(t, s.code, http.StatusAccepted)
		case <-time.After(2 * time.Second):
			t.Fatal("spawn not invoked")
		}
	})
	t.Run("400 malformed and missing", func(t *testing.T) {
		srv, h := runtimeFixture(t, supported())
		srv.restartSpawn = func() (func() error, error) { t.Fatal("spawn must not run"); return nil, nil }
		for _, body := range []string{``, `{bad`, `{}`, `{"expected_boot_id":""}`, `{"expected_boot_id":"x","extra":1}`} {
			testutil.Equal(t, postRestart(h, body).Code, http.StatusBadRequest)
		}
	})
	t.Run("401 unauthenticated", func(t *testing.T) {
		_, h := runtimeFixture(t, supported())
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/api/daemon/restart", strings.NewReader(`{"expected_boot_id":"x"}`)))
		testutil.Equal(t, w.Code, http.StatusUnauthorized)
	})
	t.Run("403 device token", func(t *testing.T) {
		srv, h := runtimeFixture(t, supported())
		srv.restartSpawn = func() (func() error, error) { t.Fatal("spawn must not run"); return nil, nil }
		plain, _, err := MintToken(srv.db, "phone")
		testutil.NoError(t, err)
		req := httptest.NewRequest("POST", "/api/daemon/restart", strings.NewReader(`{"expected_boot_id":"`+srv.bootID+`"}`))
		req.Header.Set("Authorization", "Bearer "+plain)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		testutil.Equal(t, w.Code, http.StatusForbidden)
	})
	t.Run("409 stale boot id", func(t *testing.T) {
		srv, h := runtimeFixture(t, supported())
		srv.restartSpawn = func() (func() error, error) { t.Fatal("spawn must not run"); return nil, nil }
		w := postRestart(h, `{"expected_boot_id":"old-boot"}`)
		testutil.Equal(t, w.Code, http.StatusConflict)
		assertRestartError(t, w, http.StatusConflict, "boot_id_changed", srv.bootID)
	})
	t.Run("503 unsupported mode refuses without spawning", func(t *testing.T) {
		srv, h := runtimeFixture(t, RuntimeSnapshot{RestartSupported: false})
		srv.restartSpawn = func() (func() error, error) { t.Fatal("spawn must not run"); return nil, nil }
		assertRestartError(t, postRestart(h, `{"expected_boot_id":"`+srv.bootID+`"}`), http.StatusServiceUnavailable, "restart_unsupported", "")
	})
	t.Run("500 when acceptance fails and retry is possible", func(t *testing.T) {
		srv, h := runtimeFixture(t, supported())
		srv.restartPreflight = func() error { return errors.New("no exe") }
		srv.restartSpawn = func() (func() error, error) { t.Fatal("spawn must not run"); return nil, nil }
		body := `{"expected_boot_id":"` + srv.bootID + `"}`
		assertRestartError(t, postRestart(h, body), http.StatusInternalServerError, "restart_failed", "")
		srv.restartPreflight = func() error { return nil }
		spawned := make(chan struct{}, 1)
		srv.restartSpawn = func() (func() error, error) { spawned <- struct{}{}; return func() error { return nil }, nil }
		testutil.Equal(t, postRestart(h, body).Code, http.StatusAccepted)
		<-spawned
	})
}

func TestDaemonRestart_NoDoubleRestart(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv, h := runtimeFixture(t, supported())
		var spawns atomic.Int32
		release := make(chan struct{})
		srv.restartSpawn = func() (func() error, error) {
			spawns.Add(1)
			return func() error { <-release; return nil }, nil
		}
		body := `{"expected_boot_id":"` + srv.bootID + `"}`
		codes := make(chan *httptest.ResponseRecorder, 8)
		for i := 0; i < 8; i++ {
			go func() { codes <- postRestart(h, body) }()
		}
		accepted, conflict := 0, 0
		for i := 0; i < 8; i++ {
			response := <-codes
			switch response.Code {
			case http.StatusAccepted:
				accepted++
			case http.StatusConflict:
				assertRestartError(t, response, http.StatusConflict, "restart_pending", srv.bootID)
				conflict++
			}
		}
		close(release)
		testutil.Equal(t, accepted, 1)
		testutil.Equal(t, conflict, 7)
		synctest.Wait()
		testutil.Equal(t, int(spawns.Load()), 1)
	})
}

func TestDaemonRestart_SpawnFailureClearsPending(t *testing.T) {
	srv, h := runtimeFixture(t, supported())
	fail := make(chan struct{}, 1)
	srv.restartSpawn = func() (func() error, error) { fail <- struct{}{}; return nil, errors.New("boom") }
	body := `{"expected_boot_id":"` + srv.bootID + `"}`
	testutil.Equal(t, postRestart(h, body).Code, http.StatusAccepted)
	<-fail
	deadline := time.Now().Add(2 * time.Second)
	for srv.restart.pending.Load() && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	testutil.Equal(t, srv.restart.pending.Load(), false)
}

func TestBootIDDiffersPerServer(t *testing.T) {
	a, _ := testServer(t)
	b, _ := testServer(t)
	if a.bootID == "" || a.bootID == b.bootID {
		t.Fatalf("boot ids must be unique and non-empty: %q %q", a.bootID, b.bootID)
	}
}

func TestDefaultRestartPreflightRefusesTestBinary(t *testing.T) {
	testutil.ErrorIs(t, defaultRestartPreflight(), errSpawnFromTestBinary)
}

func assertRestartError(t *testing.T, w *httptest.ResponseRecorder, status int, code, bootID string) {
	t.Helper()
	testutil.Equal(t, w.Code, status)
	want := map[string]any{"error": code}
	if bootID != "" {
		want["boot_id"] = bootID
	}
	testutil.DeepEqual(t, decodeBody(t, w), want)
}
