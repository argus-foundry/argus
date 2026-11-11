package api

import (
	"errors"
	"log/slog"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestDaemonRestartRejectsTrailingBody(t *testing.T) {
	for _, tc := range []struct{ name, suffix string }{
		{"garbage", " junk"}, {"second object", " {}"}, {"oversized suffix", strings.Repeat(" ", 5000) + "junk"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				srv, h := runtimeFixture(t, supported())
				var launches atomic.Int32
				srv.restartSpawn = func() (func() error, error) { launches.Add(1); return func() error { return nil }, nil }
				w := postRestart(h, `{"expected_boot_id":"`+srv.bootID+`"}`+tc.suffix)
				synctest.Wait()
				testutil.Equal(t, w.Code, http.StatusBadRequest)
				testutil.Equal(t, launches.Load(), int32(0))
				testutil.False(t, srv.restart.pending.Load())
			})
		})
	}
}

func TestDaemonRestartRetriesAfterSuccessorExit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		srv, h := runtimeFixture(t, supported())
		exit := make(chan struct{})
		var launches atomic.Int32
		srv.restartSpawn = func() (func() error, error) {
			launches.Add(1)
			wait, err := startRestartSuccessor(exec.Command("/usr/bin/false"))
			if err != nil {
				return nil, err
			}
			return func() error { <-exit; return wait() }, nil
		}
		body := `{"expected_boot_id":"` + srv.bootID + `"}`
		testutil.Equal(t, postRestart(h, body).Code, http.StatusAccepted)
		synctest.Wait()
		pending := postRestart(h, body)
		testutil.Equal(t, pending.Code, http.StatusConflict)
		testutil.Equal(t, decodeBody(t, pending)["error"], any("restart_pending"))
		testutil.Equal(t, decodeBody(t, pending)["boot_id"], any(srv.bootID))
		close(exit)
		synctest.Wait()
		testutil.False(t, srv.restart.pending.Load())
		testutil.Equal(t, postRestart(h, body).Code, http.StatusAccepted)
		synctest.Wait()
		testutil.Equal(t, launches.Load(), int32(2))
	})
}

func TestDaemonRestartLogsAsyncFailure(t *testing.T) {
	for _, phase := range []string{"launch", "exit"} {
		t.Run(phase, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var logs usageLogBuffer
				old := slog.Default()
				slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
				t.Cleanup(func() { slog.SetDefault(old) })
				srv, h := runtimeFixture(t, supported())
				srv.restartSpawn = func() (func() error, error) {
					if phase == "launch" {
						return nil, errors.New("launch failed marker")
					}
					return func() error { return errors.New("child exited marker") }, nil
				}
				testutil.Equal(t, postRestart(h, `{"expected_boot_id":"`+srv.bootID+`"}`).Code, http.StatusAccepted)
				synctest.Wait()
				testutil.False(t, srv.restart.pending.Load())
				if phase == "launch" {
					testutil.Contains(t, logs.String(), "successor launch failed")
					testutil.Contains(t, logs.String(), "launch failed marker")
				} else {
					testutil.Contains(t, logs.String(), "successor exited before takeover")
					testutil.Contains(t, logs.String(), "child exited marker")
				}
			})
		})
	}
}

func TestRestartSuccessorStartFailure(t *testing.T) {
	wait, err := startRestartSuccessor(exec.Command(filepath.Join(t.TempDir(), "missing")))
	testutil.Error(t, err)
	testutil.Nil(t, wait)
	wait, err = spawnRestartSuccessor()
	testutil.ErrorIs(t, err, errSpawnFromTestBinary)
	testutil.Nil(t, wait)
}
