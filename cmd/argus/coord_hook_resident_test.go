package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/testutil"
)

func TestCoordHookResident(t *testing.T) {
	for _, tc := range []struct {
		name               string
		size, last         int
		hadLast, wantNudge bool
	}{
		{"under budget", 90, 0, false, false},
		{"at budget", 100, 0, false, true},
		{"above hard stop", 200, 0, false, true},
		{"growth suppressed", 200, 195, true, false},
		{"growth reached", 210, 200, true, true},
		{"stale prior session", 100, 300, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeCoordHookEnv{getenv: map[string]string{"ARGUS_TASK_ID": "resident"}, contextSize: tc.size, lastNudged: tc.last, hadLastNudged: tc.hadLast, nudgeIncrement: 10}
			env := f.env()
			env.IsResident = func(string) (bool, error) { return true, nil }
			env.ResidentConfig = func(string) (config.ResidentConfig, error) { return config.ResidentConfig{ContextBudget: 100}, nil }
			var reply, nudge string
			env.StampResidentReply = func(_ string, text string) error { reply = text; return nil }
			env.ResidentDelivery = func(_ string, text string) error { nudge = text; return nil }
			var out, errs bytes.Buffer
			runCoordHook(strings.NewReader(`{"transcript_path":"fake","last_assistant_message":"Hello\u001b\t世界\nsecond line"}`), &out, &errs, env)
			testutil.Equal(t, reply, "Hello世界")
			testutil.Equal(t, f.stampCalled, true)
			testutil.Equal(t, f.forceRecycleCalled, false)
			testutil.Equal(t, f.readTokenTotalsCalled, false)
			testutil.Equal(t, f.budgetCalled, false)
			testutil.Equal(t, nudge != "", tc.wantNudge)
			testutil.Equal(t, f.stampLastNudgedCalled, tc.wantNudge)
			testutil.Equal(t, out.String(), "")
			if tc.wantNudge {
				testutil.Contains(t, nudge, "resident/open-loops.md")
				testutil.Contains(t, nudge, "task_recycle")
				testutil.Contains(t, nudge, "background work")
				testutil.Contains(t, nudge, "owes the user")
			}
		})
	}
}

func TestResidentBridgeMeasurementAfterRoleReadError(t *testing.T) {
	f := &fakeCoordHookEnv{getenv: map[string]string{"ARGUS_TASK_ID": "resident"}, roleKindErr: errors.New("mirror unavailable")}
	env := f.env()
	env.IsResident = func(string) (bool, error) { return true, nil }
	env.StampResidentReply = func(string, string) error { return nil }
	env.ResidentConfig = func(string) (config.ResidentConfig, error) { return config.ResidentConfig{ContextBudget: 100}, nil }
	env.BridgeMeasurement = func(string) (int, bool, bool) { return 50, true, true }
	var out, errs bytes.Buffer
	runCoordHook(strings.NewReader(`{}`), &out, &errs, env)
	testutil.Equal(t, f.stampCalled, true)
	testutil.Equal(t, f.stampedSize, 50)
	testutil.Equal(t, out.String(), "")
}

func TestResidentReplyPreview(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, text, want string }{
		{"empty", "", ""},
		{"first line", "hello\r\nignored", "hello"},
		{"controls", "a\x00\x1b\tb\u0085c", "abc"},
		{"runes", strings.Repeat("界", 205), strings.Repeat("界", 200)},
	} {
		t.Run(tc.name, func(t *testing.T) { testutil.Equal(t, residentReplyPreview(tc.text), tc.want) })
	}
}

func TestResidentReplyCapturedWithoutContext(t *testing.T) {
	f := &fakeCoordHookEnv{getenv: map[string]string{"ARGUS_TASK_ID": "resident"}, readContextErr: bytes.ErrTooLarge}
	env := f.env()
	env.IsResident = func(string) (bool, error) { return true, nil }
	var reply string
	env.StampResidentReply = func(_ string, text string) error { reply = text; return nil }
	payload, err := json.Marshal(map[string]string{"last_assistant_message": "still record me"})
	testutil.NoError(t, err)
	var out, errs bytes.Buffer
	runCoordHook(bytes.NewReader(payload), &out, &errs, env)
	testutil.Equal(t, reply, "still record me")
}

// The resident's nudge always goes through ResidentSend: the bridge budget
// endpoint only queues for hera coordinators, so it is never consulted (an
// ambiguous bridge error must not silently swallow the resident's nudge).
func TestResidentNudgeDeliveryFallback(t *testing.T) {
	for _, tc := range []struct {
		name        string
		fallbackErr error
		wantStamp   bool
	}{
		{"delivered", nil, true},
		{"unavailable", errors.New("unavailable"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &fakeCoordHookEnv{getenv: map[string]string{"ARGUS_TASK_ID": "resident"}, contextSize: 200}
			env := f.env()
			env.IsResident = func(string) (bool, error) { return true, nil }
			env.ResidentConfig = func(string) (config.ResidentConfig, error) { return config.ResidentConfig{ContextBudget: 100}, nil }
			env.StampResidentReply = func(string, string) error { return nil }
			bridged := false
			env.BridgeDelivery = func(string, int, string) (bool, error) { bridged = true; return true, errors.New("ambiguous") }
			fallback := false
			env.ResidentDelivery = func(id, text string) error {
				fallback = true
				testutil.Equal(t, id, "resident")
				testutil.Contains(t, text, "task_recycle")
				return tc.fallbackErr
			}
			var out, errs bytes.Buffer
			runCoordHook(strings.NewReader(`{}`), &out, &errs, env)
			testutil.Equal(t, out.String(), "")
			testutil.False(t, bridged)
			testutil.True(t, fallback)
			testutil.Equal(t, f.stampLastNudgedCalled, tc.wantStamp)
		})
	}
}

// Custom memory_dir must flow into the nudge text (not the "resident" default).
func TestCoordHookResidentCustomMemoryDir(t *testing.T) {
	f := &fakeCoordHookEnv{getenv: map[string]string{"ARGUS_TASK_ID": "resident"}, contextSize: 200}
	env := f.env()
	env.IsResident = func(string) (bool, error) { return true, nil }
	env.ResidentConfig = func(string) (config.ResidentConfig, error) {
		return config.ResidentConfig{ContextBudget: 100, MemoryDir: "assistant"}, nil
	}
	env.StampResidentReply = func(string, string) error { return nil }
	var nudge string
	env.ResidentDelivery = func(_ string, text string) error { nudge = text; return nil }
	var out, errs bytes.Buffer
	runCoordHook(strings.NewReader(`{}`), &out, &errs, env)
	testutil.Contains(t, nudge, "assistant/open-loops.md")
	testutil.Equal(t, strings.Contains(nudge, "resident/open-loops.md"), false)
}

// Resident failure paths must reach stderr (uxlog is a no-op in the hook
// process) while stdout stays empty.
func TestCoordHookResidentFailuresWriteStderr(t *testing.T) {
	t.Run("subject lookup", func(t *testing.T) {
		f := &fakeCoordHookEnv{getenv: map[string]string{"ARGUS_TASK_ID": "resident"}}
		env := f.env()
		env.IsResident = func(string) (bool, error) { return false, errors.New("daemon down") }
		var out, errs bytes.Buffer
		runCoordHook(strings.NewReader(`{}`), &out, &errs, env)
		testutil.Contains(t, errs.String(), "coord-hook: resident subject lookup: daemon down")
		testutil.Equal(t, out.String(), "")
	})
	t.Run("reply stamp", func(t *testing.T) {
		f := &fakeCoordHookEnv{getenv: map[string]string{"ARGUS_TASK_ID": "resident"}, readContextErr: errors.New("x")}
		env := f.env()
		env.IsResident = func(string) (bool, error) { return true, nil }
		env.StampResidentReply = func(string, string) error { return errors.New("meta write failed") }
		var out, errs bytes.Buffer
		runCoordHook(strings.NewReader(`{}`), &out, &errs, env)
		testutil.Contains(t, errs.String(), "coord-hook: stamp resident reply: meta write failed")
		testutil.Equal(t, out.String(), "")
	})
	t.Run("nudge delivery", func(t *testing.T) {
		f := &fakeCoordHookEnv{getenv: map[string]string{"ARGUS_TASK_ID": "resident"}, contextSize: 200}
		env := f.env()
		env.IsResident = func(string) (bool, error) { return true, nil }
		env.ResidentConfig = func(string) (config.ResidentConfig, error) { return config.ResidentConfig{ContextBudget: 100}, nil }
		env.StampResidentReply = func(string, string) error { return nil }
		env.ResidentDelivery = func(string, string) error { return errors.New("not running") }
		var out, errs bytes.Buffer
		runCoordHook(strings.NewReader(`{}`), &out, &errs, env)
		testutil.Contains(t, errs.String(), "coord-hook: resident nudge delivery: not running")
		testutil.Equal(t, out.String(), "")
	})
}
