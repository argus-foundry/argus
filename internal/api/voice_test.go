package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/app/agentview"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/events"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/argus-foundry/argus/internal/voice"
)

func TestVoiceSnapshotMatchesStatusCounts(t *testing.T) {
	srv, d := testServer(t)
	testutil.NoError(t, d.Add(&model.Task{Name: "p", Status: model.StatusPending}))
	ip := &model.Task{Name: "ip", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(ip))
	testutil.NoError(t, d.Add(&model.Task{Name: "ir", Status: model.StatusInReview}))
	done := &model.Task{Name: "c", Status: model.StatusComplete}
	testutil.NoError(t, d.Add(done))
	done.SetStatus(model.StatusComplete)
	testutil.NoError(t, d.Update(done))
	testutil.NoError(t, d.Add(&model.Task{Name: "arc", Status: model.StatusComplete, Archived: true}))

	run := &voiceRun{Runner: agent.NewRunner(nil), running: []string{ip.ID}, needs: []string{ip.ID}}
	srv.runner = run

	snap, err := srv.voiceSnapshot(time.Now())
	testutil.NoError(t, err)
	testutil.Equal(t, snap.Counts.Pending, 1)
	testutil.Equal(t, snap.Counts.InProgress, 1)
	testutil.Equal(t, snap.Counts.InReview, 1)
	testutil.Equal(t, snap.Counts.Complete, 1)
	testutil.Equal(t, snap.SessionsRunning, 1)
	testutil.Equal(t, snap.Counts.NeedsInput, 1)
	testutil.Equal(t, snap.NeedsInput[0], "ip")
	testutil.Equal(t, snap.Counts.RecentlyFinished, 1)

	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq(http.MethodGet, "/api/status", ""))
	testutil.Equal(t, w.Code, http.StatusOK)
	var status statusResponse
	testutil.NoError(t, json.NewDecoder(w.Body).Decode(&status))
	testutil.Equal(t, status.Sessions, sessionCounts{Running: snap.Counts.SessionsRunning, Idle: snap.Counts.SessionsIdle})
	testutil.Equal(t, status.Tasks, taskCounts{
		Pending: snap.Counts.Pending, InProgress: snap.Counts.InProgress,
		InReview: snap.Counts.InReview, Complete: snap.Counts.Complete,
	})

	got := voice.SpeakBrief(snap)
	testutil.Contains(t, got, "needs your input")
	testutil.Contains(t, got, "finished recently")
}

func TestVoiceOperatorStartStopSendResume(t *testing.T) {
	srv, d := testServer(t)
	sess := &voiceSess{}
	run := &voiceRun{Runner: agent.NewRunner(nil), sess: sess}
	srv.runner = run
	op := apiVoice{s: srv, create: func(input agent.CreateInput) (*model.Task, error) {
		return srv.createTask(input.DisplayName, input.Prompt, input.Project, "", "", "", "", false)
	}}
	ctx := context.Background()

	created, err := op.Create(ctx, "Review", "argus", "ship it")
	testutil.NoError(t, err)
	testutil.Equal(t, created.Name, "Review")
	testutil.Equal(t, created.Project, "argus")

	again, err := op.Create(ctx, "Review", "argus", "ship it")
	testutil.Error(t, err)
	testutil.Equal(t, again.ID, created.ID)

	found, err := op.Find(ctx, "", "review", "")
	testutil.NoError(t, err)
	testutil.True(t, found.Found)
	testutil.Equal(t, found.Ref.ID, created.ID)

	run.id = created.ID
	err = op.Send(ctx, created.ID, "continue")
	testutil.NoError(t, err)
	testutil.Equal(t, string(sess.written), "continue\r")
	testutil.Equal(t, sess.origin, agentview.OriginUser)

	err = op.Stop(ctx, created.ID)
	testutil.NoError(t, err)
	stopped, err := d.Get(created.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, stopped.Status, model.StatusInReview)
	testutil.Equal(t, run.stopped[0], created.ID)

	run.sess = nil
	err = op.Resume(ctx, created.ID, false)
	testutil.NoError(t, err)
	testutil.Equal(t, run.started, 1)
	resumed, err := d.Get(created.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, resumed.Status, model.StatusInProgress)
}

func TestVoiceOperatorAmbiguousName(t *testing.T) {
	srv, d := testServer(t)
	srv.runner = &voiceRun{Runner: agent.NewRunner(nil)}
	testutil.NoError(t, d.Add(&model.Task{Name: "Review", Project: "a", Status: model.StatusPending}))
	testutil.NoError(t, d.Add(&model.Task{Name: "Review", Project: "b", Status: model.StatusPending}))
	found, err := srv.VoiceOperator().Find(context.Background(), "", "Review", "")
	testutil.NoError(t, err)
	testutil.True(t, found.Ambiguous)
	found, err = srv.VoiceOperator().Find(context.Background(), "", "Review", "b")
	testutil.NoError(t, err)
	testutil.True(t, found.Found)
	testutil.Equal(t, found.Ref.Project, "b")
}

func TestVoiceFindsMultiwordNameAfterRealCreation(t *testing.T) {
	srv, d := testServer(t)
	run := &voiceRun{Runner: agent.NewRunner(nil)}
	srv.runner = run
	testutil.NoError(t, d.SetBackend("plain", config.Backend{Command: "cat"}))
	testutil.NoError(t, d.SetProject("argus", config.Project{Path: wtRepo(t), Backend: "plain"}))
	testutil.NoError(t, os.MkdirAll(agent.WorktreeDir("argus", "Daily-Review"), 0o700))
	ctx := context.Background()
	op := srv.VoiceOperator()
	created, err := op.Create(ctx, "Daily Review", "argus", "review this")
	testutil.NoError(t, err)
	testutil.Equal(t, created.Name, "Daily-Review")
	row, err := d.Get(created.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, filepath.Base(row.Worktree), "Daily-Review-1")
	found, err := op.Find(ctx, "", "Daily Review", "")
	testutil.NoError(t, err)
	testutil.Equal(t, found.Ref.ID, created.ID)
	_, err = op.Create(ctx, "Daily Review", "argus", "review again")
	testutil.Error(t, err)
	for _, name := range []string{"Build.Review", strings.Repeat("x", 31)} {
		t.Run(name, func(t *testing.T) {
			_, err := op.Create(ctx, name, "argus", "review this")
			var rejected *voice.OpError
			testutil.True(t, errors.As(err, &rejected))
			testutil.Equal(t, rejected.Code, "unstable_name")
		})
	}
	testutil.Equal(t, run.started, 1)
}

func TestVoiceFindDisambiguatesSpokenAliasesAcrossProjects(t *testing.T) {
	srv, d := testServer(t)
	raw := &model.Task{Name: "Daily Review", Project: "raw", Status: model.StatusInReview}
	slug := &model.Task{Name: "Daily-Review", Project: "slug", Status: model.StatusInReview}
	testutil.NoError(t, d.Add(raw))
	testutil.NoError(t, d.Add(slug))
	op := srv.VoiceOperator()
	ctx := context.Background()
	found, err := op.Find(ctx, "", "Daily Review", "")
	testutil.NoError(t, err)
	testutil.True(t, found.Ambiguous)
	testutil.Equal(t, found.Matches, 2)
	found, err = op.Find(ctx, "", "Daily Review", "slug")
	testutil.NoError(t, err)
	testutil.Equal(t, found.Ref.ID, slug.ID)
	testutil.NoError(t, d.SetArchived(raw.ID, true))
	found, err = op.Find(ctx, "", "Daily Review", "")
	testutil.NoError(t, err)
	testutil.Equal(t, found.Ref.ID, slug.ID)
}

func TestVoiceFindRejectsLossyAliases(t *testing.T) {
	srv, d := testServer(t)
	for _, tc := range []struct{ name, requested, stored string }{
		{"empty slug fallback", "?!", "task"},
		{"truncation", strings.Repeat("x", 31), strings.Repeat("x", 30)},
		{"punctuation substitution", "Build?Review", "Build-Review"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testutil.NoError(t, d.Add(&model.Task{Name: tc.stored, Project: tc.name, Status: model.StatusInReview}))
			got, err := srv.VoiceOperator().Find(context.Background(), "", tc.requested, tc.name)
			testutil.NoError(t, err)
			testutil.False(t, got.Found)
		})
	}
}

func TestVoiceMutationsRecheckArchivedTask(t *testing.T) {
	srv, d := testServer(t)
	run := &voiceRun{Runner: agent.NewRunner(nil)}
	srv.runner = run
	task := &model.Task{Name: "Review", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	op := srv.VoiceOperator()
	ctx := context.Background()
	ref, err := op.Find(ctx, task.ID, "", "")
	testutil.NoError(t, err)
	testutil.False(t, ref.Ref.Archived)
	testutil.NoError(t, d.SetArchived(task.ID, true))
	testutil.Equal(t, op.Stop(ctx, task.ID).Error(), "Review is archived.")
	testutil.Equal(t, op.Resume(ctx, task.ID, true).Error(), "Review is archived.")
	testutil.Equal(t, len(run.stopped), 0)
	testutil.Equal(t, run.started, 0)
}

func TestVoiceLifecycleWritesPreserveConcurrentArchiveAndRename(t *testing.T) {
	for _, command := range []string{"stop", "resume"} {
		t.Run(command, func(t *testing.T) {
			srv, d := testServer(t)
			initialStatus := model.StatusInReview
			wantStatus := model.StatusInProgress
			if command == "stop" {
				initialStatus, wantStatus = wantStatus, initialStatus
			}
			task := &model.Task{Name: "Review", Status: initialStatus, SessionID: "prior"}
			testutil.NoError(t, d.Add(task))
			ctx := context.Background()
			op := srv.VoiceOperator()
			run := &voiceRun{Runner: agent.NewRunner(nil)}
			srv.runner = run
			concurrentEdit := func() {
				testutil.NoError(t, d.SetArchived(task.ID, true))
				testutil.NoError(t, d.Rename(task.ID, "Edited"))
				_, err := d.ReplaceSessionID(task.ID, "newer")
				testutil.NoError(t, err)
			}
			if command == "stop" {
				run.beforeStop = concurrentEdit
				testutil.NoError(t, op.Stop(ctx, task.ID))
			} else {
				run.beforeStart = func(snapshot *model.Task) {
					concurrentEdit()
					snapshot.SessionID = "launched"
				}
				testutil.NoError(t, op.Resume(ctx, task.ID, false))
			}
			row, err := d.Get(task.ID)
			testutil.NoError(t, err)
			testutil.True(t, row.Archived)
			testutil.Equal(t, row.Name, "Edited")
			testutil.Equal(t, row.SessionID, "newer")
			testutil.Equal(t, row.Status, wantStatus)
			if command == "resume" {
				testutil.Equal(t, row.AgentPID, 7)
			}
		})
	}
}

func TestVoiceStopRefusesPendingRestart(t *testing.T) {
	srv, d := testServer(t)
	run := &voiceRun{Runner: agent.NewRunner(nil)}
	srv.runner = run
	task := &model.Task{Name: "Review", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	run.SetPendingRestartForTest(task.ID, true)
	router := voice.NewRouter(srv.VoiceOperator())
	ctx := context.Background()
	started := router.Do(ctx, voice.Request{Command: "stop", TaskID: task.ID, Confirm: true})
	done := router.Do(ctx, voice.Request{Command: "wait", JobID: started.JobID, Timeout: time.Second})
	testutil.Equal(t, done.Detail, "agent_restarting")
	testutil.Equal(t, len(run.stopped), 0)
	row, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, row.Status, model.StatusInProgress)
}

func TestVoiceStopDoesNotConfirmUnregisteredLaunch(t *testing.T) {
	srv, d := testServer(t)
	run := agent.NewRunner(nil)
	srv.runner = run
	task := &model.Task{Name: "Review", Status: model.StatusInProgress, Worktree: t.TempDir()}
	testutil.NoError(t, d.Add(task))
	entered := make(chan struct{})
	t.Cleanup(agent.SetPrelaunchForTest(func(ctx context.Context, _ *model.Task, _ config.Config) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}))
	finished := make(chan error, 1)
	go func() {
		_, err := run.Start(task, config.DefaultConfig(), 24, 80, false)
		finished <- err
	}()
	<-entered
	err := srv.VoiceOperator().Stop(context.Background(), task.ID)
	run.CancelPendingStarts()
	testutil.ErrorIs(t, <-finished, context.Canceled)
	var refused *voice.OpError
	testutil.True(t, errors.As(err, &refused))
	testutil.Equal(t, refused.Code, "agent_restarting")
	row, err := d.Get(task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, row.Status, model.StatusInProgress)
}

func TestVoiceStopRefusesCreationFinalization(t *testing.T) {
	srv, d := testServer(t)
	run := &voiceRun{Runner: agent.NewRunner(nil)}
	srv.runner = run
	testutil.NoError(t, d.SetBackend("plain", config.Backend{Command: "cat"}))
	testutil.NoError(t, d.SetProject("argus", config.Project{Path: wtRepo(t), Backend: "plain"}))
	entered := make(chan string, 1)
	resume := make(chan struct{})
	var release sync.Once
	t.Cleanup(func() { release.Do(func() { close(resume) }) })
	run.afterStart = func(task *model.Task) {
		entered <- task.ID
		<-resume
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	op := srv.VoiceOperator()
	finished := make(chan error, 1)
	go func() {
		_, err := op.Create(ctx, "Daily Review", "argus", "review this")
		finished <- err
	}()
	var id string
	select {
	case id = <-entered:
	case err := <-finished:
		t.Fatalf("create did not reach the launched state: %v", err)
	case <-ctx.Done():
		t.Fatal("create did not publish its session")
	}
	var busy *voice.OpError
	testutil.True(t, errors.As(op.Stop(ctx, id), &busy))
	testutil.Equal(t, busy.Code, "agent_starting")
	testutil.Equal(t, len(run.stopped), 0)

	release.Do(func() { close(resume) })
	testutil.NoError(t, <-finished)
	testutil.NoError(t, op.Stop(ctx, id))
	row, err := d.Get(id)
	testutil.NoError(t, err)
	testutil.Equal(t, row.Status, model.StatusInReview)
}

func TestVoiceLaunchReportsAmbiguousOutcome(t *testing.T) {
	srv, d := testServer(t)
	run := &voiceRun{Runner: agent.NewRunner(nil), startErr: agent.ErrStartAmbiguous}
	srv.runner = run
	task := &model.Task{Name: "Review", Status: model.StatusInReview}
	testutil.NoError(t, d.Add(task))
	op := apiVoice{s: srv, create: func(agent.CreateInput) (*model.Task, error) {
		return nil, agent.ErrStartAmbiguous
	}}
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		run  func() error
	}{
		{"create", func() error { _, err := op.Create(ctx, "Build", "p", "build"); return err }},
		{"resume", func() error { return op.Resume(ctx, task.ID, false) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var unknown *voice.OpError
			testutil.True(t, errors.As(tc.run(), &unknown))
			testutil.Equal(t, unknown.Code, "start_ambiguous")
			testutil.Contains(t, unknown.Speech, "before retrying")
		})
	}
}

func TestVoiceSendWaitsForAcknowledgement(t *testing.T) {
	srv, d := testServer(t)
	task := &model.Task{Name: "Review", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	sess := &ackVoiceSess{}
	srv.runner = &voiceRun{Runner: agent.NewRunner(nil), id: task.ID, sess: sess}
	err := srv.VoiceOperator().Send(context.Background(), task.ID, "go")
	testutil.Equal(t, sess.acknowledged, "go\r")
	testutil.Equal(t, len(sess.written), 0)
	var rejected *voice.OpError
	testutil.True(t, errors.As(err, &rejected))
	testutil.Equal(t, rejected.Code, "send_failed")
}

func TestVoiceResumeReusesHeraCloseoutGate(t *testing.T) {
	srv, d := testServer(t)
	run := &voiceRun{Runner: agent.NewRunner(nil)}
	srv.runner = run
	task, _ := mkHeraTask(t, d, db.HeraKindWorker, model.StatusInReview)
	testutil.NoError(t, d.SetMeta(task.ID, db.HeraMetaNamespace, db.HeraMetaKeyReadyToClose, "true"))
	ctx := context.Background()
	router := voice.NewRouter(srv.VoiceOperator())
	req := voice.Request{Command: "resume", TaskID: task.ID, IdempotencyKey: "revive"}
	started := router.Do(ctx, req)
	refused := router.Do(ctx, voice.Request{Command: "wait", JobID: started.JobID, Timeout: time.Second})
	testutil.Equal(t, refused.State, voice.StateNeedsConfirm)
	testutil.Equal(t, run.started, 0)
	testutil.Equal(t, readyMeta(t, d, task.ID), "true")

	req.Force = true
	started = router.Do(ctx, req)
	done := router.Do(ctx, voice.Request{Command: "wait", JobID: started.JobID, Timeout: time.Second})
	testutil.Equal(t, done.State, voice.StateDone)
	testutil.Equal(t, run.started, 1)
	awaiting, err := d.HeraWorkerAwaitingCloseout(task.ID)
	testutil.NoError(t, err)
	testutil.False(t, awaiting)
}

func TestVoiceWatcherEmitsWithoutRESTServer(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { testutil.NoError(t, d.Close()) })
	task := &model.Task{Name: "Review", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	run := &voiceRun{
		Runner: agent.NewRunner(nil), id: task.ID,
		sess: &voiceSess{idle: true, tail: blockedTail}, running: []string{task.ID}, idle: []string{task.ID},
	}
	synctest.Test(t, func(t *testing.T) {
		stop := make(chan struct{})
		t.Cleanup(func() { close(stop) })
		delivered := make(chan model.Event, 2)
		unsubscribe := events.Subscribe(func(ev model.Event) {
			if ev.Type == model.EventTypeSessionNeedsInput {
				delivered <- ev
			}
		})
		t.Cleanup(unsubscribe)
		var enabled atomic.Bool
		go WatchVoiceNeedsInput(stop, enabled.Load, d, run)
		time.Sleep(idleWatchInterval)
		synctest.Wait()
		testutil.Equal(t, len(delivered), 0)
		enabled.Store(true)
		time.Sleep(idleWatchInterval)
		synctest.Wait()
		testutil.Equal(t, len(delivered), 1)
		ev := <-delivered
		testutil.Equal(t, ev.TaskID, task.ID)
		testutil.Equal(t, string(ev.Payload), `{"needs_input":true}`)
	})
}

type voiceRun struct {
	*agent.Runner
	sess        agent.SessionHandle
	id          string
	running     []string
	idle        []string
	needs       []string
	stopped     []string
	started     int
	beforeStop  func()
	beforeStart func(*model.Task)
	afterStart  func(*model.Task)
	startErr    error
}

func (r *voiceRun) Get(id string) agent.SessionHandle {
	if r.sess != nil && (r.id == "" || r.id == id) {
		return r.sess
	}
	return nil
}

func (r *voiceRun) RunningAndIdle() ([]string, []string) { return r.running, r.idle }
func (r *voiceRun) NeedsInputIDs() []string              { return r.needs }
func (r *voiceRun) SetNeedsInputIDs([]string)            {}
func (r *voiceRun) Stop(id string) error {
	r.stopped = append(r.stopped, id)
	if r.beforeStop != nil {
		r.beforeStop()
	}
	return nil
}
func (r *voiceRun) StopSettled(id string) error {
	if r.HasPendingRestart(id) {
		return agent.ErrSessionStarting
	}
	return r.Stop(id)
}
func (r *voiceRun) Start(task *model.Task, cfg config.Config, rows, cols uint16, resume bool) (agent.SessionHandle, error) {
	sess, _, err := r.StartOrReattach(task, cfg, rows, cols, resume)
	if err == nil && r.afterStart != nil {
		r.afterStart(task)
	}
	return sess, err
}
func (r *voiceRun) StartOrReattach(task *model.Task, _ config.Config, _, _ uint16, _ bool) (agent.SessionHandle, bool, error) {
	r.started++
	if r.beforeStart != nil {
		r.beforeStart(task)
	}
	if r.startErr != nil {
		return nil, false, r.startErr
	}
	if r.sess == nil {
		r.sess = &voiceSess{}
	}
	r.id = task.ID
	return r.sess, false, nil
}

type ackVoiceSess struct {
	voiceSess
	acknowledged string
}

func (s *ackVoiceSess) WriteInputAcknowledged(_ context.Context, data []byte, _ agentview.InputOrigin) (int, error) {
	s.acknowledged = string(data)
	return 0, io.ErrClosedPipe
}

type voiceSess struct {
	idle    bool
	tail    []byte
	written []byte
	origin  agentview.InputOrigin
}

func (s *voiceSess) PID() int { return 7 }
func (s *voiceSess) WriteInput(p []byte, origin agentview.InputOrigin) (int, error) {
	s.written = append([]byte(nil), p...)
	s.origin = origin
	return len(p), nil
}
func (s *voiceSess) Resize(uint16, uint16) error                    { return nil }
func (s *voiceSess) RecentOutput() []byte                           { return nil }
func (s *voiceSess) RecentOutputTail(int) []byte                    { return s.tail }
func (s *voiceSess) RecentOutputTailWithTotal(int) ([]byte, uint64) { return nil, 0 }
func (s *voiceSess) TotalWritten() uint64                           { return 0 }
func (s *voiceSess) IsIdle() bool                                   { return s.idle }
func (s *voiceSess) LastInput() time.Time                           { return time.Time{} }
func (s *voiceSess) LastUserInput() time.Time                       { return time.Time{} }
func (s *voiceSess) Alive() bool                                    { return true }
func (s *voiceSess) PTYSize() (int, int)                            { return 80, 24 }
func (s *voiceSess) InitialPTYSize() (int, int)                     { return 80, 24 }
func (s *voiceSess) Done() <-chan struct{}                          { return make(chan struct{}) }
func (s *voiceSess) Err() error                                     { return nil }
func (s *voiceSess) WorkDir() string                                { return "" }
func (s *voiceSess) Stop() error                                    { return nil }
func (s *voiceSess) StopScoped(agent.StopScope) error               { return nil }
func (s *voiceSess) AddWriter(io.Writer)                            {}
func (s *voiceSess) AddWriterFrom(io.Writer, uint64)                {}
func (s *voiceSess) AddWriterFromTolerant(io.Writer, uint64)        {}
func (s *voiceSess) RemoveWriter(io.Writer)                         {}
