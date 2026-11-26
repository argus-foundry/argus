package voice

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const (
	// StateRunning means the job was accepted and has not finished.
	StateRunning = "running"
	// StateDone means the job finished successfully.
	StateDone = "done"
	// StateFailed means the job was refused or failed.
	StateFailed = "failed"
	// StateNeedsConfirm means a destructive command was refused until confirm is set.
	StateNeedsConfirm = "needs_confirm"

	// MaxNameRunes caps a workflow name.
	MaxNameRunes = 200
	// MaxPromptBytes caps a create prompt.
	MaxPromptBytes = 16 * 1024
	// MaxTextBytes caps one sent message.
	MaxTextBytes = 8 * 1024
	// MaxIdempotencyKey caps the retry token.
	MaxIdempotencyKey = 128
)

// MaxWait caps how long wait blocks. A longer request is shortened, and the
// answer is "Still running." when the job has not finished. Tests may shrink it.
var MaxWait = 3 * time.Second

// MaxJobs bounds remembered voice jobs. Completed jobs are dropped first.
// Tests may shrink it.
var MaxJobs = 256

// TaskRef is a workflow the voice router can name without a working directory.
type TaskRef struct {
	ID         string
	Name       string
	Project    string
	Status     string
	Live       bool
	Idle       bool
	Archived   bool
	NeedsInput bool
}

// FindResult is a name or id lookup. Ambiguous is set when more than one
// non-resident workflow matches and the caller must name a project.
type FindResult struct {
	Ref       TaskRef
	Found     bool
	Ambiguous bool
	Matches   int
}

// Operator is the Argus side of a voice command. The MCP layer does not talk
// to the runner directly; the daemon injects an implementation that reuses
// the existing task, session, and status paths.
type Operator interface {
	Snapshot(ctx context.Context, now time.Time) (Snapshot, error)
	Find(ctx context.Context, taskID, name, project string) (FindResult, error)
	Create(ctx context.Context, name, project, prompt string) (TaskRef, error)
	Stop(ctx context.Context, taskID string) error
	Resume(ctx context.Context, taskID string, force bool) error
	Send(ctx context.Context, taskID, text string) error
}

// OpError is a failure whose Speech is safe to read aloud.
type OpError struct {
	Speech string
	Code   string
}

func (e *OpError) Error() string {
	if e == nil {
		return ""
	}
	return e.Speech
}

// Request is one voice command. Fields the command does not use are ignored.
type Request struct {
	Command        string
	IdempotencyKey string
	Name           string
	Project        string
	Prompt         string
	TaskID         string
	Text           string
	Confirm        bool
	Force          bool
	JobID          string
	Timeout        time.Duration
	Now            time.Time
}

// Result is the JSON a voice tool returns. Speech is the only field to read
// aloud: one or a few plain sentences, no markdown.
type Result struct {
	OK       bool          `json:"ok"`
	Speech   string        `json:"speech"`
	JobID    string        `json:"job_id,omitempty"`
	State    string        `json:"state"`
	TaskID   string        `json:"task_id,omitempty"`
	TaskName string        `json:"task_name,omitempty"`
	Counts   *Counts       `json:"counts,omitempty"`
	Detail   string        `json:"detail,omitempty"`
	Commands []CommandSpec `json:"commands,omitempty"`
}

// Router dispatches the voice allowlist. It remembers jobs so a retried call
// with the same idempotency key does not start a second workflow.
type Router struct {
	op    Operator
	mu    sync.Mutex
	jobs  map[string]*job
	byKey map[string]*job
	order []string
}

type job struct {
	id       string
	fp       string
	key      string
	speech   string
	state    string
	taskID   string
	taskName string
	final    Result
	done     chan struct{}
	finished bool
}

// NewRouter returns a router bound to op.
func NewRouter(op Operator) *Router {
	return &Router{
		op:    op,
		jobs:  map[string]*job{},
		byKey: map[string]*job{},
	}
}

// Do runs one voice command. ctx is the lifetime of the accepted job, not of
// the caller's HTTP request: cancel it when the process shuts down.
func (r *Router) Do(ctx context.Context, req Request) Result {
	if r == nil || r.op == nil {
		return fail("Voice control is not available.", "not_configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	req.Command = strings.ToLower(strings.TrimSpace(req.Command))
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	if len(req.IdempotencyKey) > MaxIdempotencyKey {
		return fail("That idempotency key is too long.", "idempotency_key")
	}
	switch req.Command {
	case "":
		return fail("Say a command.", "command_required")
	case "kb_ingest", "kb_delete":
		return fail("That command is not available by voice.", "voice_denied")
	case "brief":
		return r.brief(ctx, req)
	case "commands":
		return listResult()
	case "start":
		return r.start(ctx, req)
	case "stop":
		return r.stop(ctx, req)
	case "resume":
		return r.resume(ctx, req)
	case "send":
		return r.send(ctx, req)
	case "status":
		return r.jobStatus(req)
	case "wait":
		return r.wait(ctx, req)
	default:
		return fail("I don't know that command.", "unknown_command")
	}
}

func (r *Router) brief(ctx context.Context, req Request) Result {
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	snap, err := r.op.Snapshot(ctx, now)
	if err != nil {
		return opFail(err)
	}
	counts := snap.Counts
	return Result{OK: true, State: StateDone, Speech: SpeakBrief(snap), Counts: &counts}
}

func listResult() Result {
	cmds := Commands()
	return Result{
		OK:       true,
		State:    StateDone,
		Speech:   sentence(qty(len(cmds)) + " voice commands are available"),
		Commands: cmds,
	}
}

func (r *Router) start(ctx context.Context, req Request) Result {
	name := strings.TrimSpace(req.Name)
	if strings.TrimSpace(req.TaskID) == "" && name == "" {
		return fail("Say which workflow to start.", "name_required")
	}
	if name != "" && len([]rune(name)) > MaxNameRunes {
		return fail("That name is too long.", "name_too_long")
	}
	found, res, ok := r.lookup(ctx, req)
	if !ok {
		if res.Detail == "not_found" && strings.TrimSpace(req.TaskID) == "" {
			return r.create(ctx, req, name)
		}
		return res
	}
	ref := found
	spoken := SpeakName(ref.Name)
	if ref.Archived {
		return fail(spoken+" is archived.", "archived")
	}
	if ref.Live && ref.NeedsInput {
		return r.complete(req, doneSpeech(spoken+" is waiting for your input.", ref))
	}
	if ref.Live && ref.Idle {
		return r.complete(req, doneSpeech(spoken+" is already running and idle.", ref))
	}
	if ref.Live {
		return r.complete(req, doneSpeech(spoken+" is already running.", ref))
	}
	return r.begin(ctx, req, "Resuming "+spoken+".", ref.ID, ref.Name, func(ctx context.Context) Result {
		if err := r.op.Resume(ctx, ref.ID, req.Confirm || req.Force); err != nil {
			return opFail(err)
		}
		return doneSpeech(spoken+" is resumed.", ref)
	})
}

func (r *Router) create(ctx context.Context, req Request, name string) Result {
	project := strings.TrimSpace(req.Project)
	prompt := strings.TrimSpace(req.Prompt)
	if project == "" || prompt == "" {
		return fail("I need a project and a prompt to start a new workflow.", "create_args")
	}
	if len(prompt) > MaxPromptBytes {
		return fail("That prompt is too long.", "prompt_too_long")
	}
	spoken := SpeakName(name)
	return r.begin(ctx, req, "Starting "+spoken+".", "", name, func(ctx context.Context) Result {
		ref, err := r.op.Create(ctx, name, project, prompt)
		if err != nil {
			return opFail(err)
		}
		return doneSpeech(SpeakName(ref.Name)+" has started.", ref)
	})
}

func (r *Router) stop(ctx context.Context, req Request) Result {
	ref, res, ok := r.lookup(ctx, req)
	if !ok {
		return res
	}
	if ref.Archived {
		return fail(SpeakName(ref.Name)+" is archived.", "archived")
	}
	spoken := SpeakName(ref.Name)
	if !req.Confirm {
		return Result{
			OK: false, State: StateNeedsConfirm, Speech: "Confirm to stop " + spoken + ".",
			TaskID: ref.ID, TaskName: ref.Name, Detail: "needs_confirm",
		}
	}
	return r.begin(ctx, req, "Stopping "+spoken+".", ref.ID, ref.Name, func(ctx context.Context) Result {
		if err := r.op.Stop(ctx, ref.ID); err != nil {
			return opFail(err)
		}
		return doneSpeech(spoken+" is stopped.", ref)
	})
}

func (r *Router) resume(ctx context.Context, req Request) Result {
	ref, res, ok := r.lookup(ctx, req)
	if !ok {
		return res
	}
	if ref.Archived {
		return fail(SpeakName(ref.Name)+" is archived.", "archived")
	}
	spoken := SpeakName(ref.Name)
	if ref.Live && ref.NeedsInput {
		return r.complete(req, doneSpeech(spoken+" is waiting for your input.", ref))
	}
	if ref.Live {
		speech := spoken + " is already running."
		if ref.Idle {
			speech = spoken + " is already running and idle."
		}
		return r.complete(req, doneSpeech(speech, ref))
	}
	return r.begin(ctx, req, "Resuming "+spoken+".", ref.ID, ref.Name, func(ctx context.Context) Result {
		if err := r.op.Resume(ctx, ref.ID, req.Confirm || req.Force); err != nil {
			return opFail(err)
		}
		return doneSpeech(spoken+" is resumed.", ref)
	})
}

func (r *Router) send(ctx context.Context, req Request) Result {
	if strings.TrimSpace(req.TaskID) == "" {
		return fail("Send needs a task id.", "task_id_required")
	}
	if strings.TrimSpace(req.Text) == "" {
		return fail("Say what to send.", "text_required")
	}
	if len(req.Text) > MaxTextBytes {
		return fail("That message is too long.", "text_too_long")
	}
	ref, res, ok := r.lookup(ctx, req)
	if !ok {
		return res
	}
	if ref.Archived {
		return fail(SpeakName(ref.Name)+" is archived.", "archived")
	}
	if !ref.Live {
		return fail(SpeakName(ref.Name)+" has no live session.", "no_session")
	}
	text := req.Text
	spoken := SpeakName(ref.Name)
	return r.begin(ctx, req, "Sending that to "+spoken+".", ref.ID, ref.Name, func(ctx context.Context) Result {
		if err := r.op.Send(ctx, ref.ID, text); err != nil {
			return opFail(err)
		}
		return doneSpeech("Sent that to "+spoken+".", ref)
	})
}

func (r *Router) lookup(ctx context.Context, req Request) (TaskRef, Result, bool) {
	if strings.TrimSpace(req.TaskID) == "" && strings.TrimSpace(req.Name) == "" {
		return TaskRef{}, fail("Say the task id or the workflow name.", "target_required"), false
	}
	found, err := r.op.Find(ctx, strings.TrimSpace(req.TaskID), strings.TrimSpace(req.Name), strings.TrimSpace(req.Project))
	if err != nil {
		return TaskRef{}, opFail(err), false
	}
	if found.Ambiguous {
		return TaskRef{}, fail("More than one workflow is named "+SpeakName(req.Name)+". Name the project.", "ambiguous"), false
	}
	if !found.Found {
		return TaskRef{}, fail("I can't find that workflow.", "not_found"), false
	}
	return found.Ref, Result{}, true
}

func (r *Router) jobStatus(req Request) Result {
	req.Timeout = 0
	return r.wait(context.Background(), req)
}

func (r *Router) wait(ctx context.Context, req Request) Result {
	id := strings.TrimSpace(req.JobID)
	if id == "" {
		return fail("Say which job to wait for.", "job_id_required")
	}
	j := r.get(id)
	if j == nil {
		return fail("I don't have that job.", "job_not_found")
	}
	timeout := req.Timeout
	if timeout < 0 {
		timeout = 0
	}
	if timeout > MaxWait {
		timeout = MaxWait
	}
	if !awaitJob(ctx, j, timeout) {
		return Result{OK: true, State: StateRunning, Speech: "Still running.", JobID: j.id, TaskID: j.taskID, TaskName: j.taskName}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.viewLocked(j)
}

func awaitJob(ctx context.Context, j *job, timeout time.Duration) bool {
	select {
	case <-j.done:
		return true
	default:
	}
	if timeout == 0 {
		return false
	}
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-j.done:
		return true
	case <-timer.C:
		return false
	case <-ctx.Done():
		return false
	}
}

func (r *Router) begin(ctx context.Context, req Request, speech, taskID, taskName string, fn func(context.Context) Result) Result {
	j, replay, bad := r.reserve(req, speech, taskID, taskName)
	if bad.Speech != "" {
		return bad
	}
	if replay.Speech != "" {
		return replay
	}
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("[voice] command panic", "command", req.Command, "panic", rec)
				r.finish(j, Result{OK: false, State: StateFailed, Speech: "That command failed.", JobID: j.id, TaskID: taskID, TaskName: taskName, Detail: "panic"})
			}
		}()
		res := fn(ctx)
		if res.State == "" {
			if res.OK {
				res.State = StateDone
			} else {
				res.State = StateFailed
			}
		}
		if res.Speech == "" {
			res.OK = false
			res.State = StateFailed
			res.Speech = "That command failed."
		}
		res.JobID = j.id
		if res.TaskID == "" {
			res.TaskID = taskID
		}
		if res.TaskName == "" {
			res.TaskName = taskName
		}
		r.finish(j, res)
	}()
	return Result{OK: true, State: StateRunning, Speech: speech, JobID: j.id, TaskID: taskID, TaskName: taskName}
}

func (r *Router) complete(req Request, res Result) Result {
	if res.State == "" {
		res.State = StateDone
	}
	res.OK = res.State == StateDone
	j, replay, bad := r.reserve(req, res.Speech, res.TaskID, res.TaskName)
	if bad.Speech != "" {
		return bad
	}
	if replay.Speech != "" {
		return replay
	}
	res.JobID = j.id
	r.finish(j, res)
	return res
}

// reserve records a job for req, or returns the job a matching idempotency key
// already owns. bad is set when the key was used for a different command or
// the job table is full. replay is set when the key matches.
func (r *Router) reserve(req Request, speech, taskID, taskName string) (j *job, replay, bad Result) {
	key := req.IdempotencyKey
	fp := fingerprint(req)
	r.mu.Lock()
	defer r.mu.Unlock()
	if key != "" {
		if existing := r.byKey[key]; existing != nil {
			if existing.fp != fp {
				return nil, Result{}, fail("That idempotency key was already used for a different command.", "idempotency_conflict")
			}
			return existing, r.viewLocked(existing), Result{}
		}
	}
	if !r.evictLocked() {
		return nil, Result{}, fail("Too many voice jobs are in flight.", "job_cap")
	}
	j = &job{
		id:       newJobID(),
		fp:       fp,
		key:      key,
		speech:   speech,
		state:    StateRunning,
		taskID:   taskID,
		taskName: taskName,
		done:     make(chan struct{}),
	}
	r.jobs[j.id] = j
	if key != "" {
		r.byKey[key] = j
	}
	r.order = append(r.order, j.id)
	return j, Result{}, Result{}
}

func (r *Router) finish(j *job, res Result) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if j.finished {
		return
	}
	j.finished = true
	j.final = res
	j.state = res.State
	close(j.done)
}

func (r *Router) viewLocked(j *job) Result {
	select {
	case <-j.done:
		res := j.final
		res.JobID = j.id
		return res
	default:
		return Result{OK: true, State: StateRunning, Speech: j.speech, JobID: j.id, TaskID: j.taskID, TaskName: j.taskName}
	}
}

func (r *Router) get(id string) *job {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.jobs[id]
}

func (r *Router) evictLocked() bool {
	for len(r.jobs) >= MaxJobs {
		evicted := false
		for i, id := range r.order {
			j := r.jobs[id]
			select {
			case <-j.done:
				delete(r.jobs, id)
				if j.key != "" && r.byKey[j.key] == j {
					delete(r.byKey, j.key)
				}
				r.order = append(r.order[:i], r.order[i+1:]...)
				evicted = true
			default:
			}
			if evicted {
				break
			}
		}
		if !evicted {
			return false
		}
	}
	return true
}

func fingerprint(req Request) string {
	body := struct {
		Command string `json:"command"`
		Name    string `json:"name"`
		Project string `json:"project"`
		Prompt  string `json:"prompt"`
		TaskID  string `json:"task_id"`
		Text    string `json:"text"`
		Confirm bool   `json:"confirm"`
		Force   bool   `json:"force"`
		JobID   string `json:"job_id"`
	}{
		Command: req.Command,
		Name:    strings.TrimSpace(req.Name),
		Project: strings.TrimSpace(req.Project),
		Prompt:  strings.TrimSpace(req.Prompt),
		TaskID:  strings.TrimSpace(req.TaskID),
		Text:    req.Text,
		Confirm: req.Confirm,
		Force:   req.Force,
		JobID:   strings.TrimSpace(req.JobID),
	}
	b, _ := json.Marshal(body)
	return string(b)
}

func newJobID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "job_" + hex.EncodeToString(b[:])
}

func doneSpeech(speech string, ref TaskRef) Result {
	return Result{OK: true, State: StateDone, Speech: speech, TaskID: ref.ID, TaskName: ref.Name}
}

func fail(speech, detail string) Result {
	return Result{OK: false, State: StateFailed, Speech: speech, Detail: detail}
}

func opFail(err error) Result {
	var oe *OpError
	if errors.As(err, &oe) && oe != nil && oe.Speech != "" {
		return Result{OK: false, State: StateFailed, Speech: oe.Speech, Detail: oe.Code}
	}
	slog.Error("[voice] command failed", "err", err)
	return Result{OK: false, State: StateFailed, Speech: "That command failed.", Detail: "operator"}
}
