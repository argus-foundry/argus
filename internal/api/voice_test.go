package api

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/app/agentview"
	"github.com/argus-foundry/argus/internal/config"
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

	got := voice.SpeakBrief(snap)
	testutil.Contains(t, got, "needs your input")
	testutil.Contains(t, got, "finished recently")
}

func TestVoiceOperatorStartStopSendResume(t *testing.T) {
	srv, d := testServer(t)
	sess := &voiceSess{}
	run := &voiceRun{Runner: agent.NewRunner(nil), sess: sess}
	srv.runner = run
	op := srv.VoiceOperator()
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

type voiceRun struct {
	*agent.Runner
	sess    *voiceSess
	id      string
	running []string
	needs   []string
	stopped []string
	started int
}

func (r *voiceRun) Get(id string) agent.SessionHandle {
	if r.sess != nil && (r.id == "" || r.id == id) {
		return r.sess
	}
	return nil
}

func (r *voiceRun) RunningAndIdle() ([]string, []string) { return r.running, nil }
func (r *voiceRun) NeedsInputIDs() []string              { return r.needs }
func (r *voiceRun) SetNeedsInputIDs([]string)            {}
func (r *voiceRun) Stop(id string) error {
	r.stopped = append(r.stopped, id)
	return nil
}
func (r *voiceRun) StartOrReattach(task *model.Task, _ config.Config, _, _ uint16, _ bool) (agent.SessionHandle, bool, error) {
	r.started++
	if r.sess == nil {
		r.sess = &voiceSess{}
	}
	r.id = task.ID
	return r.sess, false, nil
}

type voiceSess struct {
	idle    bool
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
func (s *voiceSess) RecentOutputTail(int) []byte                    { return nil }
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
