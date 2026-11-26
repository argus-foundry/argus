package voice

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

type fakeOp struct {
	mu        sync.Mutex
	find      FindResult
	findErr   error
	creates   int
	stops     int
	resumes   int
	sends     []string
	block     chan struct{}
	resumeErr error
}

func (f *fakeOp) Snapshot(context.Context, time.Time) (Snapshot, error) {
	return Snapshot{Counts: Counts{Running: 1}, Running: []string{"Review"}}, nil
}

func (f *fakeOp) Find(context.Context, string, string, string) (FindResult, error) {
	if f.findErr != nil {
		return FindResult{}, f.findErr
	}
	return f.find, nil
}

func (f *fakeOp) Create(ctx context.Context, name, project, prompt string) (TaskRef, error) {
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return TaskRef{}, ctx.Err()
		}
	}
	f.mu.Lock()
	f.creates++
	f.mu.Unlock()
	return TaskRef{ID: "t1", Name: name, Project: project}, nil
}

func (f *fakeOp) Stop(context.Context, string) error {
	f.mu.Lock()
	f.stops++
	f.mu.Unlock()
	return nil
}

func (f *fakeOp) Resume(context.Context, string, bool) error {
	f.mu.Lock()
	f.resumes++
	f.mu.Unlock()
	return f.resumeErr
}

func (f *fakeOp) Send(_ context.Context, _ string, text string) error {
	f.mu.Lock()
	f.sends = append(f.sends, text)
	f.mu.Unlock()
	return nil
}

func TestRouterBriefAndDenied(t *testing.T) {
	r := NewRouter(&fakeOp{})
	got := r.Do(context.Background(), Request{Command: "brief"})
	testutil.Equal(t, got.Speech, "Review is running.")
	testutil.Equal(t, got.Counts.Running, 1)
	testutil.True(t, got.OK)

	denied := r.Do(context.Background(), Request{Command: "kb_delete"})
	testutil.Equal(t, denied.Speech, "That command is not available by voice.")
	testutil.Equal(t, denied.Detail, "voice_denied")
	unknown := r.Do(context.Background(), Request{Command: "kb_search"})
	testutil.Equal(t, unknown.Speech, "I don't know that command.")
}

func TestRouterStartCreateAndIdempotency(t *testing.T) {
	op := &fakeOp{}
	r := NewRouter(op)
	req := Request{Command: "start", Name: "Review", Project: "argus", Prompt: "ship it", IdempotencyKey: "k1"}
	first := r.Do(context.Background(), req)
	testutil.Equal(t, first.State, StateRunning)
	testutil.Equal(t, first.Speech, "Starting Review.")
	testutil.True(t, strings.HasPrefix(first.JobID, "job_"))

	deadline := time.Now().Add(time.Second)
	var done Result
	for time.Now().Before(deadline) {
		done = r.Do(context.Background(), Request{Command: "wait", JobID: first.JobID, Timeout: 50 * time.Millisecond})
		if done.State == StateDone {
			break
		}
	}
	testutil.Equal(t, done.Speech, "Review has started.")
	testutil.Equal(t, done.State, StateDone)

	again := r.Do(context.Background(), req)
	testutil.Equal(t, again.JobID, first.JobID)
	testutil.Equal(t, again.Speech, "Review has started.")
	op.mu.Lock()
	testutil.Equal(t, op.creates, 1)
	op.mu.Unlock()

	clash := r.Do(context.Background(), Request{Command: "start", Name: "Other", Project: "argus", Prompt: "nope", IdempotencyKey: "k1"})
	testutil.Equal(t, clash.Detail, "idempotency_conflict")
}

func TestRouterWaitStillRunning(t *testing.T) {
	prev := MaxWait
	MaxWait = 40 * time.Millisecond
	t.Cleanup(func() { MaxWait = prev })

	op := &fakeOp{block: make(chan struct{})}
	r := NewRouter(op)
	first := r.Do(context.Background(), Request{Command: "start", Name: "Review", Project: "argus", Prompt: "ship it"})
	status := r.Do(context.Background(), Request{Command: "status", JobID: first.JobID})
	testutil.Equal(t, status.Speech, "Still running.")
	wait := r.Do(context.Background(), Request{Command: "wait", JobID: first.JobID, Timeout: time.Hour})
	testutil.Equal(t, wait.Speech, "Still running.")
	close(op.block)
	done := r.Do(context.Background(), Request{Command: "wait", JobID: first.JobID, Timeout: time.Second})
	testutil.Equal(t, done.State, StateDone)
}

func TestRouterStopRequiresConfirm(t *testing.T) {
	op := &fakeOp{find: FindResult{Found: true, Ref: TaskRef{ID: "t1", Name: "Review", Live: true}}}
	r := NewRouter(op)
	refused := r.Do(context.Background(), Request{Command: "stop", Name: "Review"})
	testutil.Equal(t, refused.State, StateNeedsConfirm)
	testutil.Equal(t, refused.Speech, "Confirm to stop Review.")
	op.mu.Lock()
	testutil.Equal(t, op.stops, 0)
	op.mu.Unlock()

	accepted := r.Do(context.Background(), Request{Command: "stop", Name: "Review", Confirm: true, IdempotencyKey: "stop-1"})
	testutil.Equal(t, accepted.State, StateRunning)
	confirmed := r.Do(context.Background(), Request{Command: "stop", Name: "Review", Confirm: true, IdempotencyKey: "stop-1"})
	// The retry may observe the job still running or already stopped. Either
	// way it is the same job and Stop ran once.
	testutil.Equal(t, confirmed.JobID, accepted.JobID)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		op.mu.Lock()
		n := op.stops
		op.mu.Unlock()
		if n == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("stop did not run")
}

func TestRouterSendByTaskID(t *testing.T) {
	op := &fakeOp{find: FindResult{Found: true, Ref: TaskRef{ID: "t1", Name: "Review", Live: true}}}
	r := NewRouter(op)
	missing := r.Do(context.Background(), Request{Command: "send", Name: "Review", Text: "hello"})
	testutil.Equal(t, missing.Detail, "task_id_required")

	got := r.Do(context.Background(), Request{Command: "send", TaskID: "t1", Text: "hello"})
	testutil.Equal(t, got.Speech, "Sending that to Review.")
	deadline := time.Now().Add(time.Second)
	var done Result
	for time.Now().Before(deadline) {
		done = r.Do(context.Background(), Request{Command: "status", JobID: got.JobID})
		if done.State == StateDone {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	testutil.Equal(t, done.Speech, "Sent that to Review.")
}

func TestRouterResumeClosedOut(t *testing.T) {
	op := &fakeOp{
		find:      FindResult{Found: true, Ref: TaskRef{ID: "t1", Name: "Review"}},
		resumeErr: &OpError{Speech: "Review is closed out. Confirm to revive it.", Code: "hera_closeout"},
	}
	r := NewRouter(op)
	got := r.Do(context.Background(), Request{Command: "resume", Name: "Review"})
	testutil.Equal(t, got.State, StateRunning)
	deadline := time.Now().Add(time.Second)
	var done Result
	for time.Now().Before(deadline) {
		done = r.Do(context.Background(), Request{Command: "wait", JobID: got.JobID, Timeout: 20 * time.Millisecond})
		if done.State == StateFailed {
			break
		}
	}
	testutil.Equal(t, done.Speech, "Review is closed out. Confirm to revive it.")
}

func TestRouterAlreadyRunning(t *testing.T) {
	op := &fakeOp{find: FindResult{Found: true, Ref: TaskRef{ID: "t1", Name: "Review", Live: true}}}
	r := NewRouter(op)
	got := r.Do(context.Background(), Request{Command: "start", Name: "Review", IdempotencyKey: "live"})
	testutil.Equal(t, got.State, StateDone)
	testutil.Equal(t, got.Speech, "Review is already running.")
	op.mu.Lock()
	testutil.Equal(t, op.creates, 0)
	op.mu.Unlock()
}
