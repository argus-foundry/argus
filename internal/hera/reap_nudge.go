package hera

import (
	"fmt"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// Tunables for the reap nudge watcher (add-coord-reap-triggers design D2-D4).
const (
	reapSweepInterval   = 5 * time.Minute
	reapPokeDebounce    = 45 * time.Second
	reapReevalInterval  = 15 * time.Minute
	reapRenudgeCooldown = 6 * time.Hour
	reapCoordGap        = 10 * time.Minute
	reapSweepBudget     = 90 * time.Second
)

// HeraMetaKeyReapNudged is the task_meta (namespace "hera") key recording the
// last reap nudge about a role's task: "<kind>|<task status>|<pr state>|<unix>".
// SetArchived wipes task_meta, so the record leaves with the role.
const HeraMetaKeyReapNudged = "reap_nudged"

// ReapNudgeStore is the DB surface ReapNudger needs. Satisfied by *db.DB.
type ReapNudgeStore interface {
	ArchiveStore
	ListHeraOrchestrators(includeArchived bool) ([]*db.HeraOrchestrator, error)
	ListHeraRolesByKind(orchID int64, kind db.HeraRoleKind) ([]*db.HeraRole, error)
	ListMetaByNamespace(namespace string) (map[string]map[string]string, error)
	SetMeta(taskID, namespace, key, value string) error
	SendHeraSystemNotice(roleID int64, body, tldr string) (*db.HeraMessage, error)
}

// ReapNudger tells coordinators which of their worker/freelance roles have
// become archivable (add-coord-reap-triggers). "Archivable" is exactly what
// hera_archive(dry_run=true) reports for the role; the nudger never archives,
// stops or mutates a role. It runs one goroutine: Poke schedules a sweep after
// a debounce, and a slow periodic sweep catches transitions no event announces.
type ReapNudger struct {
	store   ReapNudgeStore
	runner  ArchiveRunner
	git     GitCheck
	handoff HandoffCheck
	deliver func(*db.HeraMessage)
	now     func() time.Time

	sweepInterval, debounce, reeval, cooldown, coordGap, budget time.Duration

	pokeCh   chan struct{}
	stopCh   chan struct{}
	stopOnce sync.Once

	mu        sync.Mutex
	cache     map[int64]reapEval
	lastNudge map[int64]time.Time // coordinator role id → last nudge
}

type reapEval struct {
	fp         string
	at         time.Time
	archivable bool
	kind       db.ArchiveKind
}

type reapItem struct {
	role   *db.HeraRole
	taskID string
	kind   db.ArchiveKind
	record string
}

// NewReapNudger builds a nudger over the production archive seams. deliver
// rings the recipient's doorbell for a committed message (hera.Service.Deliver);
// nil skips delivery (the message is still stored).
func NewReapNudger(store ReapNudgeStore, runner ArchiveRunner, git GitCheck, handoff HandoffCheck, deliver func(*db.HeraMessage)) *ReapNudger {
	return &ReapNudger{
		store: store, runner: runner, git: git, handoff: handoff, deliver: deliver,
		now:           time.Now,
		sweepInterval: reapSweepInterval, debounce: reapPokeDebounce, reeval: reapReevalInterval,
		cooldown: reapRenudgeCooldown, coordGap: reapCoordGap, budget: reapSweepBudget,
		pokeCh:    make(chan struct{}, 1),
		stopCh:    make(chan struct{}),
		cache:     map[int64]reapEval{},
		lastNudge: map[int64]time.Time{},
	}
}

func reapLog(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	uxlog.Log("%s", msg)
	slog.Info(msg)
}

// Poke asks for a sweep after the debounce. Non-blocking and coalescing; safe
// on a nil receiver so callers need no hera-enabled guard.
func (n *ReapNudger) Poke() {
	if n == nil {
		return
	}
	select {
	case n.pokeCh <- struct{}{}:
	default:
	}
}

// Start runs the loop until Stop. Blocks; call in a goroutine.
func (n *ReapNudger) Start() {
	reapLog("[hera] reap-nudge: watcher starting interval=%s debounce=%s", n.sweepInterval, n.debounce)
	ticker := time.NewTicker(n.sweepInterval)
	defer ticker.Stop()
	var debounce <-chan time.Time
	var timer *time.Timer
	for {
		select {
		case <-n.stopCh:
			if timer != nil {
				timer.Stop()
			}
			return
		case <-n.pokeCh:
			// The first poke arms the timer; later pokes inside the window
			// coalesce into the same sweep.
			if debounce == nil {
				timer = time.NewTimer(n.debounce)
				debounce = timer.C
			}
		case <-debounce:
			debounce, timer = nil, nil
			n.Sweep()
		case <-ticker.C:
			n.Sweep()
		}
	}
}

// Stop ends Start. Safe to call more than once.
func (n *ReapNudger) Stop() {
	n.stopOnce.Do(func() { close(n.stopCh) })
}

// Sweep evaluates every live worker/freelance role once and sends at most one
// nudge per coordinator. Every lookup error skips (fail closed: no nudge).
func (n *ReapNudger) Sweep() {
	n.mu.Lock()
	defer n.mu.Unlock()
	orchs, err := n.store.ListHeraOrchestrators(false)
	if err != nil {
		reapLog("[hera] reap-nudge: list orchestrators failed: %v", err)
		return
	}
	prMeta, err := n.store.ListMetaByNamespace("pr")
	if err != nil {
		reapLog("[hera] reap-nudge: read pr meta failed: %v", err)
		return
	}
	heraMeta, err := n.store.ListMetaByNamespace(db.HeraMetaNamespace)
	if err != nil {
		reapLog("[hera] reap-nudge: read hera meta failed: %v", err)
		return
	}
	now := n.now()
	deadline := now.Add(n.budget)
	var evaluated, deferred, sent int
	live := map[int64]bool{}
	for _, o := range orchs {
		roles, err := n.store.ListHeraRoles(o.ID, false)
		if err != nil {
			reapLog("[hera] reap-nudge: list roles orch=%s failed: %v", o.Name, err)
			continue
		}
		var items []reapItem
		for _, r := range roles {
			task, fp, ok := n.prefilter(r, prMeta)
			if !ok {
				continue
			}
			live[r.ID] = true
			ev, cached := n.cache[r.ID]
			if !cached || ev.fp != fp || n.now().Sub(ev.at) >= n.reeval {
				if n.now().After(deadline) {
					deferred++
					continue
				}
				res := ArchiveRoleWith(n.store, n.runner, n.git, r, ArchiveOptions{DryRun: true, Handoff: n.handoff})
				ev = reapEval{fp: fp, at: n.now(), archivable: res.Archived && res.Kind != "", kind: res.Kind}
				n.cache[r.ID] = ev
				evaluated++
				if !ev.archivable {
					reapLog("[hera] reap-nudge: role=%s not archivable: %s", r.Name, res.Reason)
				}
			}
			if !ev.archivable {
				continue
			}
			record := fmt.Sprintf("%s|%s|%s", ev.kind, task.Status, prMeta[task.ID]["state"])
			if n.alreadyNudged(heraMeta[task.ID][HeraMetaKeyReapNudged], record, now) {
				continue
			}
			items = append(items, reapItem{role: r, taskID: task.ID, kind: ev.kind, record: record})
		}
		if len(items) > 0 && n.nudge(o, items, now) {
			sent++
		}
	}
	for id := range n.cache {
		if !live[id] {
			delete(n.cache, id)
		}
	}
	reapLog("[hera] reap-nudge: sweep orchs=%d evaluated=%d deferred=%d nudges=%d", len(orchs), evaluated, deferred, sent)
}

// prefilter applies the database-only gates (design D3) and returns the
// role's task plus its re-evaluation fingerprint. ok=false skips the role.
func (n *ReapNudger) prefilter(r *db.HeraRole, prMeta map[string]map[string]string) (*model.Task, string, bool) {
	if r.Kind == db.HeraKindCoordinator || r.PinnedAt != nil || r.ArchivedAt != nil || r.NukedAt != nil || r.CancelledAt != nil {
		return nil, "", false
	}
	b, err := n.store.HeraLiveBindingByRole(r.ID)
	if err != nil {
		return nil, "", false
	}
	t, err := n.store.Get(b.ArgusTaskID)
	if err != nil || t == nil || t.Archived || t.Pinned || t.IsResident() {
		return nil, "", false
	}
	if t.Status != model.StatusInReview && t.Status != model.StatusComplete {
		return nil, "", false
	}
	alive := n.runner != nil && n.runner.IsAlive(t.ID)
	if alive && !n.runner.IsIdle(t.ID) {
		return nil, "", false
	}
	roleStatus := ""
	if st, err := n.store.HeraRoleStatusFor(r.ID); err == nil && st != nil {
		roleStatus = string(st.Status)
	}
	pr := prMeta[t.ID]
	return t, fmt.Sprintf("%s|%s|%s|%s|%t", t.Status, roleStatus, pr["state"], pr["lifecycle"], alive), true
}

// alreadyNudged reports whether prev records a nudge for the same
// kind/status/PR state that is still inside the cooldown.
func (n *ReapNudger) alreadyNudged(prev, record string, now time.Time) bool {
	i := strings.LastIndex(prev, "|")
	if i < 0 || prev[:i] != record {
		return false
	}
	unix, err := strconv.ParseInt(prev[i+1:], 10, 64)
	if err != nil {
		return false
	}
	return now.Sub(time.Unix(unix, 0)) < n.cooldown
}

// nudge sends one batched notice to o's live coordinator and records each
// role as nudged. Returns true when a message was stored.
func (n *ReapNudger) nudge(o *db.HeraOrchestrator, items []reapItem, now time.Time) bool {
	coords, err := n.store.ListHeraRolesByKind(o.ID, db.HeraKindCoordinator)
	if err != nil {
		reapLog("[hera] reap-nudge: orch=%s coordinator lookup failed: %v", o.Name, err)
		return false
	}
	var coord *db.HeraRole
	for _, c := range coords {
		if c.ArchivedAt != nil || c.NukedAt != nil {
			continue
		}
		if _, err := n.store.HeraLiveBindingByRole(c.ID); err == nil {
			coord = c
			break
		}
	}
	if coord == nil {
		reapLog("[hera] reap-nudge: orch=%s has no live coordinator; %d role(s) held", o.Name, len(items))
		return false
	}
	if last, ok := n.lastNudge[coord.ID]; ok && now.Sub(last) < n.coordGap {
		reapLog("[hera] reap-nudge: coordinator=%s nudged %s ago; %d role(s) held", coord.Name, now.Sub(last).Round(time.Second), len(items))
		return false
	}
	body, tldr := reapNudgeText(items)
	msg, err := n.store.SendHeraSystemNotice(coord.ID, body, tldr)
	if err != nil {
		reapLog("[hera] reap-nudge: send to coordinator=%s failed: %v", coord.Name, err)
		return false
	}
	if n.deliver != nil {
		n.deliver(msg)
	}
	n.lastNudge[coord.ID] = now
	names := make([]string, 0, len(items))
	for _, it := range items {
		names = append(names, it.role.Name)
		if err := n.store.SetMeta(it.taskID, db.HeraMetaNamespace, HeraMetaKeyReapNudged, fmt.Sprintf("%s|%d", it.record, now.Unix())); err != nil {
			reapLog("[hera] reap-nudge: record nudge for role=%s failed: %v", it.role.Name, err)
		}
	}
	reapLog("[hera] reap-nudge: nudged coordinator=%s msg=%d roles=%s", coord.Name, msg.ID, strings.Join(names, ","))
	return true
}

// reapNudgeText builds the notice body and a tldr within db.HeraMaxTldrLen.
func reapNudgeText(items []reapItem) (body, tldr string) {
	sorted := append([]reapItem(nil), items...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].role.Name < sorted[j].role.Name })
	noun := "roles"
	if len(sorted) == 1 {
		noun = "role"
	}
	labeled := make([]string, 0, len(sorted))
	names := make([]string, 0, len(sorted))
	for _, it := range sorted {
		labeled = append(labeled, fmt.Sprintf("%s (%s)", it.role.Name, strings.ReplaceAll(string(it.kind), "_", " ")))
		names = append(names, it.role.Name)
	}
	body = fmt.Sprintf("%s%d %s ready to archive: %s. Run hera_archive(cwd=$PWD) to retire them; it re-checks each one, and a refusal just means it changed. The daemon never archives on its own.",
		db.HeraSystemNoticePrefix, len(sorted), noun, strings.Join(labeled, ", "))
	tldr = fmt.Sprintf("%d %s ready to archive: %s", len(sorted), noun, strings.Join(names, ", "))
	if r := []rune(tldr); len(tldr) > db.HeraMaxTldrLen {
		for len(string(r)) > db.HeraMaxTldrLen-len("…") {
			r = r[:len(r)-1]
		}
		tldr = string(r) + "…"
	}
	return body, tldr
}
