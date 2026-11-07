package hera

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/model"
)

const (
	LifecycleKindBounce       = "bounce"
	LifecycleKindForceRecycle = "force_recycle"

	LifecycleStateAwaitingHandoff = "awaiting_handoff"
	LifecycleStateRestarting      = "restarting"
	LifecycleStateRestarted       = "restarted"
	LifecycleStateFailed          = "failed"

	lifecycleSubjectRole = "hera_role"
)

// BounceInstruction is the system input asking a worker/freelance session to
// hand off and request its own recycle. It never mentions "coordinator": the
// same wording applies to either role kind.
const BounceInstruction = "You've been asked to bounce (fresh session, same task/worktree/branch). " +
	"Please wrap up: call hera_status with handoff_note summarizing what's done, the current state, and what's next, and request_recycle=true, so your session restarts fresh once you go idle."

var (
	ErrLifecycleWrongKind    = errors.New("lifecycle: role kind does not support this operation")
	ErrLifecycleStaleBinding = errors.New("lifecycle: role has no live binding")
	ErrLifecycleNoSession    = errors.New("lifecycle: role has no live session")
	ErrLifecycleRequestReuse = db.ErrOperationRequestReuse
	ErrLifecycleActive       = db.ErrOperationActive
)

// LifecycleStore is the DB surface the lifecycle service needs; *db.DB satisfies it.
type LifecycleStore interface {
	RecycleStore
	Get(taskID string) (*model.Task, error)
	CreateOperation(op *db.Operation) (*db.Operation, bool, error)
	Operation(id string) (*db.Operation, error)
	OperationByRequestID(requestID string) (*db.Operation, error)
	ActiveOperation(subjectType, subjectID, kind string) (*db.Operation, error)
	TransitionOperation(id, from, to, errMsg string, active bool) (*db.Operation, bool, error)
	ListActiveOperations(kind, state string) ([]*db.Operation, error)
}

// LifecycleSessions is the live-session seam: liveness check and system input.
type LifecycleSessions interface {
	SessionAlive(taskID string) bool
	SendSystemInput(taskID string, text string) error
}

// LifecycleOperation is the wire/event view of an operation receipt.
type LifecycleOperation struct {
	OperationID string  `json:"operation_id"`
	RoleID      int64   `json:"role_id"`
	TaskID      string  `json:"task_id"`
	Kind        string  `json:"kind"`
	State       string  `json:"state"`
	Error       *string `json:"error"`
}

// LifecycleView converts a stored receipt into its wire form.
func LifecycleView(op *db.Operation) LifecycleOperation {
	roleID, _ := strconv.ParseInt(op.SubjectID, 10, 64)
	v := LifecycleOperation{OperationID: op.ID, RoleID: roleID, TaskID: op.TaskID, Kind: op.Kind, State: op.State}
	if op.Error != "" {
		e := op.Error
		v.Error = &e
	}
	return v
}

// LifecycleService is the shared worker-bounce / coordinator-force-recycle
// implementation behind both the REST routes and the TUI rail actions.
type LifecycleService struct {
	store    LifecycleStore
	sessions LifecycleSessions
	recycler RecycleRunner
	emit     func(taskID string, payload LifecycleOperation)
	newID    func() string
	async    func(func())
}

// NewLifecycleService builds the service. Recycles run on their own goroutine
// unless SetSynchronous is used.
func NewLifecycleService(store LifecycleStore, sessions LifecycleSessions, recycler RecycleRunner) *LifecycleService {
	return &LifecycleService{
		store:    store,
		sessions: sessions,
		recycler: recycler,
		emit: func(taskID string, p LifecycleOperation) {
			events.Emit(model.EventTypeHeraLifecycleChanged, taskID, p)
		},
		newID: NewOperationID,
		async: func(f func()) { go f() },
	}
}

// SetEmitter overrides event emission (tests).
func (s *LifecycleService) SetEmitter(f func(taskID string, p LifecycleOperation)) { s.emit = f }

// SetSynchronous runs forced recycles inline (tests).
func (s *LifecycleService) SetSynchronous() { s.async = func(f func()) { f() } }

func lifecycleFingerprint(kind string, roleID int64) string {
	return fmt.Sprintf("%s|%d", kind, roleID)
}

// replay returns the existing receipt for requestID, ErrLifecycleRequestReuse
// when it names a different request, or (nil, nil) when it is new.
func (s *LifecycleService) replay(requestID, fingerprint string) (*db.Operation, error) {
	op, err := s.store.OperationByRequestID(requestID)
	if errors.Is(err, db.ErrOperationNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if op.Fingerprint != fingerprint {
		return nil, ErrLifecycleRequestReuse
	}
	return op, nil
}

// resolveRole validates the role's kind, live binding and live session. The
// binding is re-read here rather than trusted from the caller.
func (s *LifecycleService) resolveRole(roleID int64, kinds ...db.HeraRoleKind) (*db.HeraBinding, error) {
	role, err := s.store.HeraRole(roleID)
	if err != nil {
		return nil, err
	}
	ok := false
	for _, k := range kinds {
		if role.Kind == k {
			ok = true
		}
	}
	if !ok {
		return nil, ErrLifecycleWrongKind
	}
	b, err := s.store.HeraLiveBindingByRole(roleID)
	if errors.Is(err, db.ErrHeraNotFound) {
		return nil, ErrLifecycleStaleBinding
	}
	if err != nil {
		return nil, err
	}
	if !s.sessions.SessionAlive(b.ArgusTaskID) {
		return nil, ErrLifecycleNoSession
	}
	return b, nil
}

func (s *LifecycleService) begin(requestID, kind, state string, roleID int64, taskID string) (*db.Operation, bool, error) {
	op, created, err := s.store.CreateOperation(&db.Operation{
		ID: s.newID(), RequestID: requestID, Fingerprint: lifecycleFingerprint(kind, roleID),
		Kind: kind, SubjectType: lifecycleSubjectRole, SubjectID: strconv.FormatInt(roleID, 10),
		TaskID: taskID, State: state,
	})
	if err != nil {
		return nil, false, err
	}
	if created {
		slog.Info("[hera-lifecycle] accepted", "op", op.ID, "kind", kind, "role", roleID, "task", taskID)
		s.emit(op.TaskID, LifecycleView(op))
	}
	return op, created, nil
}

func (s *LifecycleService) transition(op *db.Operation, to, errMsg string, active bool) *db.Operation {
	next, changed, err := s.store.TransitionOperation(op.ID, op.State, to, errMsg, active)
	if err != nil {
		slog.Warn("[hera-lifecycle] transition persist failed", "op", op.ID, "to", to, "err", err)
		return op
	}
	if changed {
		slog.Info("[hera-lifecycle] transition", "op", op.ID, "kind", op.Kind, "from", op.State, "to", to)
		s.emit(next.TaskID, LifecycleView(next))
	}
	return next
}

// Bounce asks a live worker/freelance session to hand off and recycle itself.
// It writes the instruction exactly once and never kills the session; the
// operation stays awaiting_handoff until the role's own self-service recycle
// is observed via ObserveRecycle.
func (s *LifecycleService) Bounce(requestID string, roleID int64) (*db.Operation, error) {
	fp := lifecycleFingerprint(LifecycleKindBounce, roleID)
	if op, err := s.replay(requestID, fp); err != nil || op != nil {
		return op, err
	}
	b, err := s.resolveRole(roleID, db.HeraKindWorker, db.HeraKindFreelance)
	if err != nil {
		return nil, err
	}
	op, created, err := s.begin(requestID, LifecycleKindBounce, LifecycleStateAwaitingHandoff, roleID, b.ArgusTaskID)
	if err != nil || !created {
		return op, err
	}
	if err := s.sessions.SendSystemInput(b.ArgusTaskID, BounceInstruction); err != nil {
		slog.Warn("[hera-lifecycle] bounce instruction write failed", "op", op.ID, "task", b.ArgusTaskID, "err", err)
		return s.transition(op, LifecycleStateFailed, "could not deliver the bounce instruction", false), nil
	}
	return op, nil
}

// ForceRecycle immediately kills and restarts a coordinator's session on the
// same task, seeded from mission, plan state and any handoff note. wait=true
// runs the restart inline (TUI); otherwise it completes in the background.
func (s *LifecycleService) ForceRecycle(requestID string, roleID int64, wait bool) (*db.Operation, error) {
	fp := lifecycleFingerprint(LifecycleKindForceRecycle, roleID)
	if op, err := s.replay(requestID, fp); err != nil || op != nil {
		return op, err
	}
	b, err := s.resolveRole(roleID, db.HeraKindCoordinator)
	if err != nil {
		return nil, err
	}
	op, created, err := s.begin(requestID, LifecycleKindForceRecycle, LifecycleStateRestarting, roleID, b.ArgusTaskID)
	if err != nil || !created {
		return op, err
	}
	run := func() *db.Operation { return s.executeRecycle(op, roleID, b.ArgusTaskID) }
	if wait {
		return run(), nil
	}
	s.async(func() { run() })
	return op, nil
}

func (s *LifecycleService) executeRecycle(op *db.Operation, roleID int64, taskID string) *db.Operation {
	sessionID := ""
	if t, err := s.store.Get(taskID); err == nil && t != nil {
		sessionID = t.SessionID
	}
	if err := RecycleCoord(s.store, s.recycler, roleID, sessionID, RecycleHumanForced); err != nil {
		slog.Warn("[hera-lifecycle] forced recycle failed", "op", op.ID, "role", roleID, "task", taskID, "err", err)
		return s.transition(op, LifecycleStateFailed, "restart failed", false)
	}
	return s.transition(op, LifecycleStateRestarted, "", false)
}

// ObserveRecycle advances an awaiting bounce when the role's self-service
// recycle restarted (err == nil) or failed.
func (s *LifecycleService) ObserveRecycle(roleID int64, err error) {
	op, gErr := s.store.ActiveOperation(lifecycleSubjectRole, strconv.FormatInt(roleID, 10), LifecycleKindBounce)
	if gErr != nil || op.State != LifecycleStateAwaitingHandoff {
		return
	}
	if err != nil {
		slog.Warn("[hera-lifecycle] self-service recycle failed", "op", op.ID, "role", roleID, "err", err)
		s.transition(op, LifecycleStateFailed, "restart failed", false)
		return
	}
	s.transition(op, LifecycleStateRestarted, "", false)
}

// Get returns an operation, scoped to roleID.
func (s *LifecycleService) Get(roleID int64, operationID string) (*db.Operation, error) {
	op, err := s.store.Operation(operationID)
	if err != nil {
		return nil, err
	}
	if op.SubjectType != lifecycleSubjectRole || op.SubjectID != strconv.FormatInt(roleID, 10) {
		return nil, db.ErrOperationNotFound
	}
	return op, nil
}

// Recover fails restarts that were in flight when the daemon last stopped.
// Awaiting-handoff bounces survive: the pending_recycle flag is durable.
func (s *LifecycleService) Recover() {
	ops, err := s.store.ListActiveOperations(LifecycleKindForceRecycle, LifecycleStateRestarting)
	if err != nil {
		slog.Warn("[hera-lifecycle] recover list failed", "err", err)
		return
	}
	for _, op := range ops {
		s.transition(op, LifecycleStateFailed, "interrupted by daemon restart", false)
	}
}

// NewOperationID returns a random UUIDv4 string.
func NewOperationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err)
	}
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
