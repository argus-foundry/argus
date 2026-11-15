package api

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestResidentApprovalFlashNeverSticksWaiting(t *testing.T) {
	srv, d := testServer(t)
	r, _, err := d.EnsureResidentTask(filepath.Join(t.TempDir(), "resident"))
	testutil.NoError(t, err)
	srv.SetResidentPermissionHook(func(id string) bool { return id == r.ID })
	state := newIdleWatcherState()
	tail := []byte("Bash command\nDo you want to proceed?\n❯ 1. Yes\n  2. No\n")
	srv.detectNeedsInputTick(state, []string{r.ID}, []string{r.ID}, func(string) []byte { return tail })
	testutil.False(t, srv.ResidentWaiting())
	srv.ResidentPermissionAnswered(r.ID)
	srv.detectNeedsInputTick(state, []string{r.ID}, []string{r.ID}, func(string) []byte { return tail })
	testutil.False(t, srv.ResidentWaiting())
	srv.detectNeedsInputTick(state, []string{r.ID}, nil, func(string) []byte { return idleTail })
	testutil.False(t, srv.ResidentWaiting())
}

func TestResidentApprovalSuppressionBoundedAndLaunchScoped(t *testing.T) {
	srv, _ := testServer(t)
	now := time.Now()
	testutil.True(t, srv.suppressResidentPermission("r", "launch", true, now))
	testutil.False(t, srv.suppressResidentPermission("r", "launch", true, now.Add(11*time.Second)))
	srv.residentApproval.answeredUntil = now.Add(time.Minute)
	testutil.True(t, srv.suppressResidentPermission("r", "launch", true, now.Add(12*time.Second)))
	testutil.False(t, srv.suppressResidentPermission("r", "launch", false, now.Add(12*time.Second)))
	// A restart cannot inherit the old answered grace period.
	testutil.True(t, srv.suppressResidentPermission("r", "new", true, now.Add(13*time.Second)))
	testutil.False(t, srv.suppressResidentPermission("r", "new", true, now.Add(24*time.Second)))
}
