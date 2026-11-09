package db

import (
	"errors"
	"testing"

	"github.com/argus-foundry/argus/internal/testutil"
)

func TestTaskDeleteJobs(t *testing.T) {
	d, err := OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })

	j, created, err := d.CreateTaskDeleteJob(TaskDeleteJob{ID: "a", IdempotencyKey: "k", RequestHash: "h", Status: "queued", Results: "{}"})
	testutil.NoError(t, err)
	testutil.True(t, created)
	testutil.Equal(t, j.ID, "a")

	t.Run("same key returns stored job", func(t *testing.T) {
		got, created, err := d.CreateTaskDeleteJob(TaskDeleteJob{ID: "b", IdempotencyKey: "k", RequestHash: "other", Status: "queued", Results: "{}"})
		testutil.NoError(t, err)
		testutil.Equal(t, created, false)
		testutil.Equal(t, got.ID, "a")
		testutil.Equal(t, got.RequestHash, "h")
	})
	t.Run("blank keys never dedupe", func(t *testing.T) {
		for _, id := range []string{"c", "d"} {
			_, created, err := d.CreateTaskDeleteJob(TaskDeleteJob{ID: id, RequestHash: "h", Status: "queued", Results: "{}"})
			testutil.NoError(t, err)
			testutil.True(t, created)
		}
	})
	t.Run("save and list unfinished", func(t *testing.T) {
		testutil.NoError(t, d.SaveTaskDeleteJob("a", "complete", `{"x":1}`))
		got, err := d.GetTaskDeleteJob("a")
		testutil.NoError(t, err)
		testutil.Equal(t, got.Status, "complete")
		testutil.Equal(t, got.Results, `{"x":1}`)
		open, err := d.ListUnfinishedTaskDeleteJobs()
		testutil.NoError(t, err)
		testutil.Equal(t, len(open), 2)
	})
	t.Run("unknown job", func(t *testing.T) {
		_, err := d.GetTaskDeleteJob("zzz")
		testutil.True(t, errors.Is(err, ErrDeleteJobNotFound))
		testutil.True(t, errors.Is(d.SaveTaskDeleteJob("zzz", "x", "{}"), ErrDeleteJobNotFound))
	})
}

func TestTaskHoldsLiveHeraCoordinatorBinding(t *testing.T) {
	d, err := OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	orch, err := d.CreateHeraOrchestrator("o", "master")
	testutil.NoError(t, err)
	_, _, err = d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "c", Kind: HeraKindCoordinator}, "t1", "/w1")
	testutil.NoError(t, err)
	_, _, err = d.CreateHeraRoleWithBinding(CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "w", Kind: HeraKindWorker}, "t2", "/w2")
	testutil.NoError(t, err)
	for id, want := range map[string]bool{"t1": true, "t2": false, "none": false} {
		got, err := d.TaskHoldsLiveHeraCoordinatorBinding(id)
		testutil.NoError(t, err)
		testutil.Equal(t, got, want)
	}
}
