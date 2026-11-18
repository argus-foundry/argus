package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestRunCodexSessionHook(t *testing.T) {
	payload := `{"session_id":"01a1ebbc","cwd":"/w","hook_event_name":"SessionStart","source":"startup"}`
	t.Run("reports the session id for the task", func(t *testing.T) {
		var gotTask, gotSID, gotSource string
		err := runCodexSessionHook(strings.NewReader(payload), "t1", func(task, sid, source string) error {
			gotTask, gotSID = task, sid
			gotSource = source
			return nil
		})
		testutil.NoError(t, err)
		testutil.Equal(t, gotTask, "t1")
		testutil.Equal(t, gotSID, "01a1ebbc")
		testutil.Equal(t, gotSource, "startup")
	})
	t.Run("startup and clear starts are reported, others are not", func(t *testing.T) {
		for source, want := range map[string]bool{"startup": true, "": true, "clear": true, "resume": false, "compact": false} {
			called := false
			body := `{"session_id":"01a1ebbc","source":"` + source + `"}`
			if source == "" {
				body = `{"session_id":"01a1ebbc"}`
			}
			testutil.NoError(t, runCodexSessionHook(strings.NewReader(body), "t1", func(string, string, string) error { called = true; return nil }))
			testutil.Equal(t, called, want)
		}
	})
	t.Run("no task id means not an argus session", func(t *testing.T) {
		called := false
		err := runCodexSessionHook(strings.NewReader(payload), "", func(string, string, string) error { called = true; return nil })
		testutil.NoError(t, err)
		testutil.True(t, !called)
	})
	t.Run("bad payloads and report failures are returned not fatal", func(t *testing.T) {
		report := func(string, string, string) error { return errors.New("down") }
		testutil.Error(t, runCodexSessionHook(strings.NewReader("{"), "t", report))
		testutil.Error(t, runCodexSessionHook(strings.NewReader(`{"cwd":"/w"}`), "t", report))
		testutil.Error(t, runCodexSessionHook(strings.NewReader(payload), "t", report))
	})
}
