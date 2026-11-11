// Package bouncerecovery implements add-bounce-recovery: after a supervisor
// restart kills agent sessions, the daemon records what each was doing (the
// ledger, in task_meta namespace "bounce"), decides who to wake, and resumes
// them in throttled order with an [argus] system note. See
// openspec/changes/add-bounce-recovery/design.md.
package bouncerecovery

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/argus-foundry/argus/internal/transcriptstate"
)

// Namespace is the task_meta namespace holding the recovery ledger.
const Namespace = "bounce"

// Status is an entry's recovery status.
type Status string

const (
	StatusPending    Status = "pending"
	StatusResumed    Status = "resumed"
	StatusDormant    Status = "dormant"
	StatusDelegated  Status = "delegated"
	StatusFailed     Status = "failed"
	StatusExpired    Status = "expired"
	StatusSuperseded Status = "superseded"
)

// Role is the hera role class of a killed task.
const (
	RoleCoordinator = "coordinator"
	RoleWorker      = "worker"
	RoleFreelance   = "freelance"
	RolePlain       = "plain"
)

// MaxAttempts bounds resume attempts per bounce.
const MaxAttempts = 3

// Entry is one killed session's recovery record.
type Entry struct {
	TaskID    string
	Status    Status
	KilledAt  time.Time
	State     transcriptstate.Kind
	Question  string
	Options   []string
	Role      string // RoleCoordinator / RoleWorker / RoleFreelance / RolePlain
	RoleName  string // hera role name ("" for plain tasks)
	TaskName  string
	OrchID    int64  // orchestrator of the (first) coordinator or worker binding
	CoordTask string // supervising coordinator's task ID ("" when none)
	Depth     int    // orchestrator nesting depth (0 = top level)
	Attempts  int
	NextAt    time.Time // earliest next attempt (zero = now)
	BounceID  string
}

func (e Entry) toMeta() map[string]string {
	opts, _ := json.Marshal(e.Options)
	m := map[string]string{
		"status":     string(e.Status),
		"killed_at":  strconv.FormatInt(e.KilledAt.Unix(), 10),
		"state":      string(e.State),
		"question":   e.Question,
		"options":    string(opts),
		"role":       e.Role,
		"role_name":  e.RoleName,
		"task_name":  e.TaskName,
		"orch_id":    strconv.FormatInt(e.OrchID, 10),
		"coord_task": e.CoordTask,
		"depth":      strconv.Itoa(e.Depth),
		"attempts":   strconv.Itoa(e.Attempts),
		"next_at":    "0",
		"bounce_id":  e.BounceID,
	}
	if !e.NextAt.IsZero() {
		m["next_at"] = strconv.FormatInt(e.NextAt.Unix(), 10)
	}
	return m
}

func entryFromMeta(taskID string, m map[string]string) (Entry, bool) {
	if m["status"] == "" || m["killed_at"] == "" {
		return Entry{}, false
	}
	e := Entry{
		TaskID:    taskID,
		Status:    Status(m["status"]),
		State:     transcriptstate.Kind(m["state"]),
		Question:  m["question"],
		Role:      m["role"],
		RoleName:  m["role_name"],
		TaskName:  m["task_name"],
		CoordTask: m["coord_task"],
		BounceID:  m["bounce_id"],
	}
	if unix, err := strconv.ParseInt(m["killed_at"], 10, 64); err == nil {
		e.KilledAt = time.Unix(unix, 0)
	}
	if unix, err := strconv.ParseInt(m["next_at"], 10, 64); err == nil && unix > 0 {
		e.NextAt = time.Unix(unix, 0)
	}
	e.OrchID, _ = strconv.ParseInt(m["orch_id"], 10, 64)
	e.Depth, _ = strconv.Atoi(m["depth"])
	e.Attempts, _ = strconv.Atoi(m["attempts"])
	_ = json.Unmarshal([]byte(m["options"]), &e.Options)
	return e, true
}

// Store is the DB surface the ledger needs (satisfied by *db.DB).
type Store interface {
	SetMetaBatch(taskID, namespace string, entries map[string]string) error
	SetMeta(taskID, namespace, key, value string) error
	ListMetaByNamespace(namespace string) (map[string]map[string]string, error)
}

// Save writes e as a whole.
func Save(s Store, e Entry) error {
	return s.SetMetaBatch(e.TaskID, Namespace, e.toMeta())
}

// SetStatus updates only the status (and attempt bookkeeping) of an entry.
func SetStatus(s Store, e *Entry, st Status) error {
	e.Status = st
	return s.SetMetaBatch(e.TaskID, Namespace, map[string]string{
		"status":   string(st),
		"attempts": strconv.Itoa(e.Attempts),
		"next_at":  nextAtString(e.NextAt),
	})
}

func nextAtString(t time.Time) string {
	if t.IsZero() {
		return "0"
	}
	return strconv.FormatInt(t.Unix(), 10)
}

// LoadAll reads every ledger entry keyed by task ID.
func LoadAll(s Store) (map[string]*Entry, error) {
	byTask, err := s.ListMetaByNamespace(Namespace)
	if err != nil {
		return nil, err
	}
	out := make(map[string]*Entry, len(byTask))
	for id, m := range byTask {
		if e, ok := entryFromMeta(id, m); ok {
			e := e
			out[id] = &e
		}
	}
	return out, nil
}
