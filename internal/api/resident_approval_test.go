package api

import (
	"path/filepath"
	"sync"
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
	srv.ResidentPermissionAnswered(r.ID, "Bash", []byte(`{"command":"echo probe"}`))
	srv.detectNeedsInputTick(state, []string{r.ID}, []string{r.ID}, func(string) []byte { return tail })
	testutil.False(t, srv.ResidentWaiting())
	srv.detectNeedsInputTick(state, []string{r.ID}, nil, func(string) []byte { return idleTail })
	testutil.False(t, srv.ResidentWaiting())
}

func TestResidentApprovalSuppressionBoundedAndLaunchScoped(t *testing.T) {
	srv, _ := testServer(t)
	now := time.Now()
	testutil.True(t, srv.suppressResidentPermission("r", "launch", "Bash command echo probe", now))
	testutil.False(t, srv.suppressResidentPermission("r", "launch", "Bash command echo probe", now.Add(11*time.Second)))
	srv.residentApproval.answeredCall = "Bash command echo probe"
	testutil.True(t, srv.suppressResidentPermission("r", "launch", "Bash command echo probe", now.Add(12*time.Second)))
	testutil.False(t, srv.suppressResidentPermission("r", "launch", "Bash command echo probe", now.Add(16*time.Second)))
	testutil.False(t, srv.suppressResidentPermission("r", "launch", "", now.Add(16*time.Second)))
	// A restart cannot inherit the old answered grace period.
	testutil.True(t, srv.suppressResidentPermission("r", "new", "Bash command echo probe", now.Add(17*time.Second)))
	testutil.False(t, srv.suppressResidentPermission("r", "new", "Bash command echo probe", now.Add(28*time.Second)))
}

func TestResidentApprovalUnrelatedHooksNeverHideNativePrompt(t *testing.T) {
	srv, _ := testServer(t)
	now := time.Now()
	dialog := "requires permission: native sandbox network"
	for second := range 30 {
		srv.ResidentPermissionAnswered("r", "mcp__other__write", []byte(`{"body":"other"}`))
		suppressed := srv.suppressResidentPermission("r", "", dialog, now.Add(time.Duration(second)*time.Second))
		testutil.Equal(t, suppressed, second < 10)
	}
	// A different dialog is immediately visible, even inside the grace interval.
	testutil.False(t, srv.suppressResidentPermission("r", "", "Bash command new request", now.Add(time.Second)))
}

func TestResidentApprovalNativePromptBecomesWaitingDespiteHookTraffic(t *testing.T) {
	srv, d := testServer(t)
	r, _, err := d.EnsureResidentTask(filepath.Join(t.TempDir(), "resident"))
	testutil.NoError(t, err)
	srv.SetResidentPermissionHook(func(string) bool { return true })
	state := newIdleWatcherState()
	tail := func(string) []byte {
		return []byte("Permission rule: sandbox network\nDo you want to proceed?\n❯ 1. Yes\n  2. No\n")
	}
	srv.detectNeedsInputTick(state, []string{r.ID}, []string{r.ID}, tail)
	testutil.False(t, srv.ResidentWaiting())
	srv.residentApproval.candidateSince = time.Now().Add(-16 * time.Second)
	for range 5 {
		srv.ResidentPermissionAnswered(r.ID, "mcp__unrelated__write", []byte(`{"body":"other"}`))
		srv.detectNeedsInputTick(state, []string{r.ID}, []string{r.ID}, tail)
	}
	testutil.True(t, srv.ResidentWaiting())
}

func TestResidentApprovalAnsweredMatchesOnlyItsChooser(t *testing.T) {
	srv, _ := testServer(t)
	now := time.Now()
	dialog := "Bash command\necho probe\nDo you want to proceed?"
	testutil.True(t, srv.suppressResidentPermission("r", "", dialog, now))
	srv.ResidentPermissionAnswered("r", "Bash", []byte(`{"command":"echo other"}`))
	testutil.False(t, srv.suppressResidentPermission("r", "", dialog, now.Add(11*time.Second)))
	srv.ResidentPermissionAnswered("r", "Bash", []byte(`{"command":"echo probe"}`))
	testutil.True(t, srv.suppressResidentPermission("r", "", dialog, now.Add(12*time.Second)))
	for second := 15; second < 30; second++ {
		srv.ResidentPermissionAnswered("r", "Bash", []byte(`{"command":"echo probe"}`))
		testutil.False(t, srv.suppressResidentPermission("r", "", dialog, now.Add(time.Duration(second)*time.Second)))
	}
}

func TestResidentApprovalHookConcurrentConfiguration(t *testing.T) {
	srv, _ := testServer(t)
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 100 {
			srv.SetResidentPermissionHook(func(string) bool { return true })
		}
	})
	wg.Go(func() {
		state := newIdleWatcherState()
		for range 100 {
			srv.filterResidentApprovalWaiting(state, map[string]bool{}, func(string) []byte { return nil })
		}
	})
	wg.Wait()
}
