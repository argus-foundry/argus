package events

import (
	"encoding/json"
	"sync"
	"testing"

	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// recordingSink captures emitted events for inspection.
type recordingSink struct {
	mu  sync.Mutex
	got []model.Event
}

func (r *recordingSink) Emit(ev model.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, ev)
}

func (r *recordingSink) events() []model.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]model.Event, len(r.got))
	copy(out, r.got)
	return out
}

func TestEmit_NoSink_NoOp(t *testing.T) {
	// Ensure no sink is registered before the test runs.
	prev := SetSink(nil)
	t.Cleanup(func() { SetSink(prev) })

	// Must not panic.
	Emit(model.EventTypeTaskCreated, "abc", map[string]string{"name": "x"})
}

func TestEmit_DeliversToSink(t *testing.T) {
	sink := &recordingSink{}
	prev := SetSink(sink)
	t.Cleanup(func() { SetSink(prev) })

	Emit(model.EventTypeTaskRenamed, "task-1", map[string]string{"from": "old", "to": "new"})

	got := sink.events()
	testutil.Equal(t, len(got), 1)
	testutil.Equal(t, got[0].Type, model.EventTypeTaskRenamed)
	testutil.Equal(t, got[0].TaskID, "task-1")
	if got[0].At.IsZero() {
		t.Error("expected At to be stamped")
	}

	var payload map[string]string
	testutil.NoError(t, json.Unmarshal(got[0].Payload, &payload))
	testutil.Equal(t, payload["from"], "old")
	testutil.Equal(t, payload["to"], "new")
}

func TestEmit_NilPayloadOmitsBytes(t *testing.T) {
	sink := &recordingSink{}
	prev := SetSink(sink)
	t.Cleanup(func() { SetSink(prev) })

	Emit(model.EventTypeSessionStarted, "task-2", nil)

	got := sink.events()
	testutil.Equal(t, len(got), 1)
	if got[0].Payload != nil {
		t.Errorf("expected nil payload, got %q", string(got[0].Payload))
	}
}

func TestEmit_MarshalErrorDroppedSilently(t *testing.T) {
	sink := &recordingSink{}
	prev := SetSink(sink)
	t.Cleanup(func() { SetSink(prev) })

	// channels are not marshalable.
	Emit(model.EventTypeMessageSent, "t", make(chan int))

	got := sink.events()
	testutil.Equal(t, len(got), 1)
	if got[0].Payload != nil {
		t.Errorf("expected nil payload on marshal failure, got %q", string(got[0].Payload))
	}
}

func TestSetSink_NilClears(t *testing.T) {
	sink := &recordingSink{}
	prev := SetSink(sink)
	t.Cleanup(func() { SetSink(prev) })

	SetSink(nil)
	Emit("anything", "", nil)
	testutil.Equal(t, len(sink.events()), 0)
}

func TestSetSink_ReturnsPrevious(t *testing.T) {
	a := &recordingSink{}
	b := &recordingSink{}

	prev0 := SetSink(a)
	t.Cleanup(func() { SetSink(prev0) })
	prev1 := SetSink(b)
	t.Cleanup(func() { SetSink(a) }) // restored explicitly below

	if prev1 != a {
		t.Errorf("expected previous sink == a")
	}
	// Reset to prev0 so other tests see the clean state.
	SetSink(prev0)
	if got := SetSink(prev0); got != prev0 {
		// no-op restore — but exercises the round-trip
		_ = got
	}
}

func TestEmit_ResidentEventsRewrittenToResidentNamespace(t *testing.T) {
	sink := &recordingSink{}
	prev := SetSink(sink)
	t.Cleanup(func() { SetSink(prev) })

	MarkResident("res-evt-1")
	Emit(model.EventTypeTaskUpdated, "res-evt-1", nil)
	Emit(model.EventTypeSessionIdle, "res-evt-1", nil)
	Emit(model.EventTypeResidentCreated, "res-evt-1", nil) // already namespaced
	Emit(model.EventTypeTaskUpdated, "ordinary", nil)

	got := sink.events()
	testutil.Equal(t, len(got), 4)
	testutil.Equal(t, got[0].Type, "resident.task.updated")
	testutil.Equal(t, got[1].Type, "resident.session.idle")
	testutil.Equal(t, got[2].Type, "resident.created")
	testutil.Equal(t, got[3].Type, model.EventTypeTaskUpdated)
}

func TestMarkResident_EmptyIDIgnored(t *testing.T) {
	sink := &recordingSink{}
	prev := SetSink(sink)
	t.Cleanup(func() { SetSink(prev) })

	MarkResident("")
	Emit(model.EventTypeSessionIdle, "", nil)
	testutil.Equal(t, sink.events()[0].Type, model.EventTypeSessionIdle)
}
