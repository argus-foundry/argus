package db

import (
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

type resultPublicationSink struct {
	db      *DB
	once    atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (s *resultPublicationSink) Emit(ev model.Event) {
	if ev.Type == model.EventTypeTaskUpdated && s.once.CompareAndSwap(false, true) {
		close(s.entered)
		<-s.release
	}
	_, _ = s.db.InsertEvent(&ev)
}

func TestResultPublication_ConcurrentClear(t *testing.T) {
	for _, writer := range []string{"SetResult", "Update"} {
		t.Run(writer, func(t *testing.T) {
			d := testDB(t)
			task := &model.Task{ID: "ordered", Name: "n"}
			testutil.NoError(t, d.Add(task))
			sink := &resultPublicationSink{db: d, entered: make(chan struct{}), release: make(chan struct{})}
			prev := events.SetSink(sink)
			t.Cleanup(func() { events.SetSink(prev) })
			first, second := make(chan error, 1), make(chan error, 1)
			task.Result = `{"failed":true,"reason":"first"}`
			go func() {
				if writer == "Update" {
					first <- d.Update(task)
				} else {
					first <- d.SetResult(task.ID, task.Result)
				}
			}()
			<-sink.entered
			firstAt := d.ResultAt(task.ID)
			go func() { second <- d.SetResult(task.ID, `{"ok":true}`) }()
			secondFinished := false
			select {
			case err := <-second:
				testutil.NoError(t, err)
				secondFinished = true
			case <-time.After(100 * time.Millisecond):
			}
			close(sink.release)
			testutil.NoError(t, <-first)
			if !secondFinished {
				testutil.NoError(t, <-second)
			}
			stored, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, stored.Result, `{"ok":true}`)
			rows, err := d.EventsSince(0, 0)
			testutil.NoError(t, err)
			var failures []*model.TaskFailure
			for _, row := range rows {
				if row.Type == model.EventTypeTaskFailureChanged {
					var payload struct{ Failure *model.TaskFailure }
					testutil.NoError(t, json.Unmarshal(row.Payload, &payload))
					failures = append(failures, payload.Failure)
				}
			}
			testutil.Equal(t, len(failures), 2)
			testutil.DeepEqual(t, failures, []*model.TaskFailure{
				{Reason: "first", Source: "task_result", RecordedAt: firstAt.UTC().Format(time.RFC3339)}, nil,
			})
		})
	}
}
