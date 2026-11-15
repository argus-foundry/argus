package api

import (
	"net/http"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentTimerList(t *testing.T) {
	s, d := testServer(t)
	w := serve(s.routes(), "GET", "/api/resident/timers", "")
	testutil.Equal(t, w.Code, http.StatusOK)
	testutil.Contains(t, w.Body.String(), `"timers":[]`)
	_, err := d.SetResidentTimer("check Sam", time.Now().Add(time.Hour), "", "", time.Now())
	testutil.NoError(t, err)
	w = serve(s.routes(), "GET", "/api/resident/timers", "")
	testutil.Equal(t, w.Code, http.StatusOK)
	testutil.Contains(t, w.Body.String(), "check Sam")
	testutil.Contains(t, w.Body.String(), "due_at")
	testutil.NoError(t, d.EnsureResidentMemoryTidy("tidy memory", "30 2 * * *", "UTC", time.Now()))
	w = serve(s.routes(), "GET", "/api/resident/timers", "")
	testutil.Equal(t, w.Code, http.StatusOK)
	testutil.Contains(t, w.Body.String(), `"builtin":"memory_tidy"`)
	testutil.Contains(t, w.Body.String(), "check Sam")
}
