package db

import (
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

// planSink records hera.plan_changed payloads and calls back into the DB while
// emitting, which would deadlock if emission happened under d.mu. observed
// holds what the DB reported at emission time.
type planSink struct {
	mu       sync.Mutex
	d        *DB
	orchs    []int64
	observed []int
}

func (s *planSink) Emit(ev model.Event) {
	if ev.Type != model.EventTypeHeraPlanChanged {
		return
	}
	var p struct {
		OrchestratorID int64 `json:"orchestrator_id"`
	}
	_ = json.Unmarshal(ev.Payload, &p)
	blocks, _ := s.d.ListHeraBlocks(p.OrchestratorID)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.orchs = append(s.orchs, p.OrchestratorID)
	s.observed = append(s.observed, len(blocks))
}

func (s *planSink) take() []int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.orchs
	s.orchs, s.observed = nil, nil
	return out
}

func installPlanSink(t *testing.T, d *DB) *planSink {
	t.Helper()
	s := &planSink{d: d}
	prev := events.SetSink(s)
	t.Cleanup(func() { events.SetSink(prev) })
	return s
}

func TestPlanChanged_EmittedAfterCommitForEachMutation(t *testing.T) {
	d := testDB(t)
	orch, err := d.CreateHeraOrchestrator("o1", "")
	testutil.NoError(t, err)
	other, err := d.CreateHeraOrchestrator("o2", "")
	testutil.NoError(t, err)
	sink := installPlanSink(t, d)

	done := make(chan struct{})
	go func() {
		defer close(done)

		a, err := d.CreateHeraPlannedRole(CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "a"})
		testutil.NoError(t, err)
		b, err := d.CreateHeraPlannedRole(CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "b"})
		testutil.NoError(t, err)
		testutil.DeepEqual(t, sink.take(), []int64{orch.ID, orch.ID})

		testutil.NoError(t, d.AddHeraBlock(b.ID, a.ID))
		testutil.DeepEqual(t, sink.take(), []int64{orch.ID})

		testutil.NoError(t, d.RemoveHeraBlock(b.ID, a.ID))
		testutil.DeepEqual(t, sink.take(), []int64{orch.ID})

		testutil.NoError(t, d.UpdateHeraPlannedNode(a.ID, "new prompt", ""))
		testutil.NoError(t, d.CancelHeraPlannedNode(b.ID))
		testutil.NoError(t, d.UpsertHeraRoleStatus(a.ID, HeraStatusWorking))
		testutil.NoError(t, d.RenameHeraRole(a.ID, "a2"))
		testutil.NoError(t, d.ArchiveHeraRole(a.ID))
		testutil.NoError(t, d.UnarchiveHeraRole(a.ID))
		testutil.DeepEqual(t, sink.take(), []int64{orch.ID, orch.ID, orch.ID, orch.ID, orch.ID, orch.ID})

		_, err = d.CreateHeraPlan(other.ID, []HeraPlannedNodeSpec{{Name: "x"}, {Name: "y"}},
			[]HeraBlockSpec{{BlockedNodeIdx: 1, BlockerNodeIdx: 0}})
		testutil.NoError(t, err)
		testutil.DeepEqual(t, sink.take(), []int64{other.ID})
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("deadlock: plan_changed emitted while holding the DB lock")
	}
}

func TestPlanChanged_BlockVisibleAtEmission(t *testing.T) {
	d := testDB(t)
	orch, _ := d.CreateHeraOrchestrator("o1", "")
	a, _ := d.CreateHeraPlannedRole(CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "a"})
	b, _ := d.CreateHeraPlannedRole(CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "b"})
	sink := installPlanSink(t, d)
	testutil.NoError(t, d.AddHeraBlock(b.ID, a.ID))
	sink.mu.Lock()
	defer sink.mu.Unlock()
	testutil.DeepEqual(t, sink.observed, []int{1})
}

func TestPlanChanged_NotEmittedOnFailure(t *testing.T) {
	d := testDB(t)
	orch, _ := d.CreateHeraOrchestrator("o1", "")
	a, _ := d.CreateHeraPlannedRole(CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "a"})
	b, _ := d.CreateHeraPlannedRole(CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "b"})
	testutil.NoError(t, d.AddHeraBlock(b.ID, a.ID))
	sink := installPlanSink(t, d)

	if err := d.AddHeraBlock(a.ID, b.ID); err == nil {
		t.Fatal("expected cycle error")
	}
	if err := d.AddHeraBlock(a.ID, a.ID); err == nil {
		t.Fatal("expected self-edge error")
	}
	testutil.Equal(t, len(sink.take()), 0)
}

func TestPlanChanged_BindingAndTaskStatus(t *testing.T) {
	d := testDB(t)
	orch, _ := d.CreateHeraOrchestrator("o1", "")
	task := &model.Task{Name: "t", Project: "p", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	sink := installPlanSink(t, d)

	role, binding, err := d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "w", Kind: HeraKindWorker}, task.ID, "/tmp/wt")
	testutil.NoError(t, err)
	testutil.DeepEqual(t, sink.take(), []int64{orch.ID})

	testutil.NoError(t, d.SetStatus(task.ID, model.StatusInReview))
	testutil.DeepEqual(t, sink.take(), []int64{orch.ID})

	testutil.NoError(t, d.SetStatus(task.ID, model.StatusInReview))
	testutil.Equal(t, len(sink.take()), 0)

	testutil.NoError(t, d.EndHeraBinding(binding.ID, "test"))
	testutil.DeepEqual(t, sink.take(), []int64{orch.ID})

	_ = role
	testutil.NoError(t, d.Delete(task.ID))
	testutil.DeepEqual(t, sink.take(), []int64{orch.ID})
}

func TestPlanChanged_NukeOrchestratorAndDeleteRole(t *testing.T) {
	d := testDB(t)
	orch, _ := d.CreateHeraOrchestrator("o1", "")
	r, _ := d.CreateHeraPlannedRole(CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "a"})
	sink := installPlanSink(t, d)
	testutil.NoError(t, d.DeleteHeraRole(r.ID))
	testutil.DeepEqual(t, sink.take(), []int64{orch.ID})
	testutil.NoError(t, d.NukeHeraOrchestrator(orch.ID))
	testutil.DeepEqual(t, sink.take(), []int64{orch.ID})
}
