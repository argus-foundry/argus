package voice

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/testutil"
)

type fakeOp struct {
	mu          sync.Mutex
	find        FindResult
	findErr     error
	creates     int
	stops       int
	resumes     int
	sends       []string
	block       chan struct{}
	resumeErr   error
	panicCreate bool
	snapErr     error
}

func (f *fakeOp) Snapshot(context.Context, time.Time) (Snapshot, error) {
	if f.snapErr != nil {
		return Snapshot{}, f.snapErr
	}
	return Snapshot{Counts: Counts{Running: 1}, Running: []string{"Review"}}, nil
}

func (f *fakeOp) Find(context.Context, string, string, string) (FindResult, error) {
	if f.findErr != nil {
		return FindResult{}, f.findErr
	}
	return f.find, nil
}

func (f *fakeOp) Create(ctx context.Context, name, project, prompt string) (TaskRef, error) {
	if f.panicCreate {
		panic("create boom")
	}
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

func (f *fakeOp) Resume(_ context.Context, _ string, force bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.resumes++
	if force {
		return nil
	}
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

func TestRouterRejectsSuspendInputBeforeStartingJob(t *testing.T) {
	op := &fakeOp{find: FindResult{Found: true, Ref: TaskRef{ID: "t1", Name: "Review", Live: true}}}
	r := NewRouter(op)
	got := r.Do(context.Background(), Request{Command: "send", TaskID: "t1", Text: "before\x1aafter"})
	testutil.Equal(t, got.Detail, "unsupported_input")
	testutil.Equal(t, got.JobID, "")

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	testutil.NoError(t, r.Shutdown(ctx))
	op.mu.Lock()
	defer op.mu.Unlock()
	testutil.Equal(t, len(op.sends), 0)
}

func TestRouterReplaysSendAfterSessionExits(t *testing.T) {
	op := &fakeOp{find: FindResult{Found: true, Ref: TaskRef{ID: "t1", Name: "Review", Live: true}}}
	r := NewRouter(op)
	ctx := context.Background()
	req := Request{Command: "send", TaskID: "t1", Text: "hello", IdempotencyKey: "turn"}
	first := r.Do(ctx, req)
	testutil.True(t, first.JobID != "")
	done := r.Do(ctx, Request{Command: "wait", JobID: first.JobID, Timeout: time.Second})
	testutil.Equal(t, done.State, StateDone)

	op.find.Ref.Live = false
	req.Prompt = "unused"
	testutil.DeepEqual(t, r.Do(ctx, req), done)
	req.Text = "different message"
	testutil.Equal(t, r.Do(ctx, req).Detail, "idempotency_conflict")
	testutil.DeepEqual(t, op.sends, []string{"hello"})
}

func TestRouterResumeClosedOut(t *testing.T) {
	for _, command := range []string{"resume", "start"} {
		t.Run(command, func(t *testing.T) {
			op := &fakeOp{
				find:      FindResult{Found: true, Ref: TaskRef{ID: "t1", Name: "Review"}},
				resumeErr: &OpError{Speech: "Review is closed out. Confirm to revive it.", Code: "hera_closeout"},
			}
			r := NewRouter(op)
			ctx := context.Background()
			req := Request{Command: command, Name: "Review", IdempotencyKey: "revive"}
			first := r.Do(ctx, req)
			testutil.Equal(t, first.State, StateRunning)
			refused := r.Do(ctx, Request{Command: "wait", JobID: first.JobID, Timeout: time.Second})
			testutil.Equal(t, refused.State, StateNeedsConfirm)
			testutil.Equal(t, refused.Speech, "Review is closed out. Confirm to revive it.")

			req.Confirm = true
			next := r.Do(ctx, req)
			testutil.Equal(t, next.State, StateRunning)
			testutil.True(t, next.JobID != first.JobID)
			done := r.Do(ctx, Request{Command: "wait", JobID: next.JobID, Timeout: time.Second})
			testutil.Equal(t, done.State, StateDone)
			testutil.Equal(t, op.resumes, 2)
			testutil.DeepEqual(t, r.Do(ctx, Request{Command: "status", JobID: first.JobID}), refused)
		})
	}
}

func TestRouterShutdownDrainsAcceptedWork(t *testing.T) {
	op := &fakeOp{block: make(chan struct{})}
	r := NewRouter(op)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	req := Request{Command: "start", Name: "Review", Project: "argus", Prompt: "ship it"}
	got := r.Do(ctx, req)
	testutil.Equal(t, got.State, StateRunning)
	cancel()
	testutil.NoError(t, r.Shutdown(context.Background()))
	done := r.Do(ctx, Request{Command: "status", JobID: got.JobID})
	testutil.Equal(t, done.State, StateFailed)
	testutil.Equal(t, r.Do(context.Background(), req).Detail, "canceled")
}

func TestRouterResumesDeadWorkflow(t *testing.T) {
	op := &fakeOp{find: FindResult{Found: true, Ref: TaskRef{ID: "t1", Name: "Review"}}}
	r := NewRouter(op)
	for _, cmd := range []string{"resume", "start"} {
		got := r.Do(context.Background(), Request{Command: cmd, Name: "Review", Confirm: true})
		deadline := time.Now().Add(time.Second)
		var done Result
		for time.Now().Before(deadline) {
			done = r.Do(context.Background(), Request{Command: "wait", JobID: got.JobID, Timeout: 20 * time.Millisecond})
			if done.State == StateDone {
				break
			}
		}
		testutil.Equal(t, done.Speech, "Review is resumed.")
	}
	op.mu.Lock()
	testutil.Equal(t, op.resumes, 2)
	op.mu.Unlock()
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

func TestRouterEdges(t *testing.T) {
	live := FindResult{Found: true, Ref: TaskRef{ID: "t1", Name: "Review", Live: true}}
	for _, tc := range []struct {
		name string
		op   *fakeOp
		req  Request
		want string
	}{
		{"empty", &fakeOp{}, Request{}, "Say a command."},
		{"commands", &fakeOp{}, Request{Command: "commands"}, "Eight voice commands are available."},
		{"long key", &fakeOp{}, Request{Command: "brief", IdempotencyKey: strings.Repeat("k", 129)}, "That idempotency key is too long."},
		{"long name", &fakeOp{}, Request{Command: "start", Name: strings.Repeat("n", 201)}, "That name is too long."},
		{"ambiguous", &fakeOp{find: FindResult{Ambiguous: true}}, Request{Command: "stop", Name: "Review", Confirm: true}, "Name the project or say the task id."},
		{"archived", &fakeOp{find: FindResult{Found: true, Ref: TaskRef{Name: "Review", Archived: true}}}, Request{Command: "stop", Name: "Review", Confirm: true}, "Review is archived."},
		{"needs input", &fakeOp{find: FindResult{Found: true, Ref: TaskRef{Name: "Review", Live: true, NeedsInput: true}}}, Request{Command: "resume", Name: "Review"}, "Review is waiting for your input."},
		{"idle", &fakeOp{find: liveIdle()}, Request{Command: "start", Name: "Review"}, "Review is already running and idle."},
		{"no prompt", &fakeOp{}, Request{Command: "start", Name: "Review", Project: "argus"}, "I need a project and a prompt"},
		{"long prompt", &fakeOp{}, Request{Command: "start", Name: "Review", Project: "argus", Prompt: strings.Repeat("p", MaxPromptBytes+1)}, "That prompt is too long."},
		{"send name", &fakeOp{find: live}, Request{Command: "send", Name: "Review", Text: "hi"}, "Send needs a task id."},
		{"send empty", &fakeOp{find: live}, Request{Command: "send", TaskID: "t1"}, "Say what to send."},
		{"send long", &fakeOp{find: live}, Request{Command: "send", TaskID: "t1", Text: strings.Repeat("x", MaxTextBytes+1)}, "That message is too long."},
		{"send dead", &fakeOp{find: FindResult{Found: true, Ref: TaskRef{ID: "t1", Name: "Review"}}}, Request{Command: "send", TaskID: "t1", Text: "hi"}, "has no live session."},
		{"missing target", &fakeOp{}, Request{Command: "stop", Confirm: true}, "Say the task id or the workflow name."},
		{"missing job", &fakeOp{}, Request{Command: "wait"}, "Say which job to wait for."},
		{"unknown job", &fakeOp{}, Request{Command: "status", JobID: "job_missing"}, "I don't have that job."},
		{"db down", &fakeOp{findErr: errors.New("db down")}, Request{Command: "stop", Name: "Review", Confirm: true}, "That command failed."},
		{"brief down", &fakeOp{snapErr: errors.New("db down")}, Request{Command: "brief"}, "That command failed."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := NewRouter(tc.op).Do(context.Background(), tc.req)
			testutil.Contains(t, got.Speech, tc.want)
		})
	}

	testutil.Equal(t, (*Router)(nil).Do(context.Background(), Request{Command: "brief"}).Speech, "Voice control is not available.")
	testutil.Equal(t, (*OpError)(nil).Error(), "")
	testutil.Equal(t, (&OpError{Speech: "no"}).Error(), "no")
	testutil.Equal(t, opFail(&OpError{}).Speech, "That command failed.")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	blocked := &fakeOp{block: make(chan struct{})}
	t.Cleanup(func() { close(blocked.block) })
	r := NewRouter(blocked)
	running := r.Do(context.Background(), Request{Command: "start", Name: "Review", Project: "argus", Prompt: "go"})
	still := r.Do(ctx, Request{Command: "wait", JobID: running.JobID, Timeout: -1})
	testutil.Equal(t, still.Speech, "Still running.")
}

func liveIdle() FindResult {
	return FindResult{Found: true, Ref: TaskRef{ID: "t1", Name: "Review", Live: true, Idle: true}}
}

func TestMaxJobsDefault(t *testing.T) {
	testutil.Equal(t, MaxJobs, 256)
}

func TestRouterJobCapAndPanic(t *testing.T) {
	prev := MaxJobs
	t.Cleanup(func() { MaxJobs = prev })
	MaxJobs = 1

	blocked := &fakeOp{block: make(chan struct{})}
	r := NewRouter(blocked)
	first := r.Do(context.Background(), Request{Command: "start", Name: "One", Project: "argus", Prompt: "a"})
	second := r.Do(context.Background(), Request{Command: "start", Name: "Two", Project: "argus", Prompt: "b"})
	testutil.Equal(t, second.Detail, "job_cap")
	close(blocked.block)
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if r.Do(context.Background(), Request{Command: "status", JobID: first.JobID}).State == StateDone {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	MaxJobs = 1
	doneOp := &fakeOp{find: FindResult{Found: true, Ref: TaskRef{ID: "t1", Name: "Review", Live: true}}}
	limited := NewRouter(doneOp)
	a := limited.Do(context.Background(), Request{Command: "start", Name: "Review"})
	b := limited.Do(context.Background(), Request{Command: "start", Name: "Review"})
	testutil.Equal(t, a.State, StateDone)
	testutil.Equal(t, b.State, StateDone)
	testutil.Equal(t, limited.Do(context.Background(), Request{Command: "status", JobID: a.JobID}).Detail, "job_not_found")

	panicky := &fakeOp{panicCreate: true}
	pr := NewRouter(panicky)
	job := pr.Do(context.Background(), Request{Command: "start", Name: "Review", Project: "argus", Prompt: "go"})
	deadline = time.Now().Add(time.Second)
	var failed Result
	for time.Now().Before(deadline) {
		failed = pr.Do(context.Background(), Request{Command: "wait", JobID: job.JobID, Timeout: 20 * time.Millisecond})
		if failed.State == StateFailed {
			break
		}
	}
	testutil.Equal(t, failed.Speech, "That command failed.")
}
