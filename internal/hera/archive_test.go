package hera

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

type fakeArchiveRunner struct {
	alive, idle, blocked bool
	stopped              []string
	stopErr              error
}

func (f *fakeArchiveRunner) IsAlive(string) bool         { return f.alive }
func (f *fakeArchiveRunner) IsIdle(string) bool          { return f.idle }
func (f *fakeArchiveRunner) BlockedOnPrompt(string) bool { return f.blocked }
func (f *fakeArchiveRunner) Stop(id string) error {
	f.stopped = append(f.stopped, id)
	return f.stopErr
}

func gitOK(*model.Task) (bool, string)  { return true, "" }
func gitBad(*model.Task) (bool, string) { return false, "open PR" }

func archDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// archWorker adds a complete worker task+role in orch.
func archWorker(t *testing.T, d *db.DB, orchID int64, name string, status model.Status) (*model.Task, *db.HeraRole) {
	t.Helper()
	task := &model.Task{Name: name, Status: status, Project: "p", Worktree: "/wt/" + name, Branch: "b-" + name}
	testutil.NoError(t, d.Add(task))
	role, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{
		OrchestratorID: orchID, Name: name, Kind: db.HeraKindWorker, ArgusProject: "p",
	}, task.ID, task.Worktree)
	testutil.NoError(t, err)
	// SetStatus may be a no-op for in_progress; set explicitly.
	testutil.NoError(t, d.SetStatus(task.ID, status))
	return task, role
}

func TestArchivableVerdict(t *testing.T) {
	d := archDB(t)
	orch, err := d.CreateHeraOrchestrator("o", "master")
	testutil.NoError(t, err)

	t.Run("complete and landed is archivable", func(t *testing.T) {
		_, r := archWorker(t, d, orch.ID, "ok", model.StatusComplete)
		v := ArchivableVerdict(d, gitOK, r)
		testutil.Equal(t, v.Archivable, true)
	})
	t.Run("not complete refuses", func(t *testing.T) {
		_, r := archWorker(t, d, orch.ID, "wip", model.StatusInReview)
		v := ArchivableVerdict(d, gitOK, r)
		testutil.Equal(t, v.Archivable, false)
		testutil.Contains(t, v.Reason, "not complete")
	})
	t.Run("git not landed refuses", func(t *testing.T) {
		_, r := archWorker(t, d, orch.ID, "pr", model.StatusComplete)
		v := ArchivableVerdict(d, gitBad, r)
		testutil.Equal(t, v.Archivable, false)
		testutil.Contains(t, v.Reason, "open PR")
	})
	t.Run("nil git check refuses a task with a branch", func(t *testing.T) {
		_, r := archWorker(t, d, orch.ID, "nogit", model.StatusComplete)
		v := ArchivableVerdict(d, nil, r)
		testutil.Equal(t, v.Archivable, false)
	})
	t.Run("blocked role refuses", func(t *testing.T) {
		_, r := archWorker(t, d, orch.ID, "blk", model.StatusComplete)
		testutil.NoError(t, d.UpsertHeraRoleStatus(r.ID, db.HeraStatusBlocked))
		v := ArchivableVerdict(d, gitOK, r)
		testutil.Contains(t, v.Reason, "blocked")
	})
	t.Run("coordinator refuses", func(t *testing.T) {
		v := ArchivableVerdict(d, gitOK, &db.HeraRole{Kind: db.HeraKindCoordinator})
		testutil.Contains(t, v.Reason, "coordinator")
	})
	t.Run("live dependent refuses until done", func(t *testing.T) {
		_, blocker := archWorker(t, d, orch.ID, "blocker", model.StatusComplete)
		dep, err := d.CreateHeraPlannedRole(db.CreateHeraRoleInput{
			OrchestratorID: orch.ID, Name: "dep", Kind: db.HeraKindWorker, ArgusProject: "p", Prompt: "later",
		})
		testutil.NoError(t, err)
		testutil.NoError(t, d.AddHeraBlock(dep.ID, blocker.ID))
		v := ArchivableVerdict(d, gitOK, blocker)
		testutil.Contains(t, v.Reason, "depends")
		testutil.NoError(t, d.UpsertHeraRoleStatus(dep.ID, db.HeraStatusDone))
		v = ArchivableVerdict(d, gitOK, blocker)
		testutil.Equal(t, v.Archivable, true)
	})
}

func TestArchiveRole(t *testing.T) {
	t.Run("archives task and role, keeps worktree, stops idle session", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		task, r := archWorker(t, d, orch.ID, "w", model.StatusComplete)
		run := &fakeArchiveRunner{alive: true, idle: true}
		res := ArchiveRole(d, run, gitOK, r, false)
		testutil.Equal(t, res.Archived, true)
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Archived, true)
		testutil.Equal(t, got.Worktree, task.Worktree) // never reclaimed
		testutil.Equal(t, got.Branch, task.Branch)
		role, _ := d.HeraRole(r.ID)
		testutil.Equal(t, role.ArchivedAt != nil, true)
		testutil.DeepEqual(t, run.stopped, []string{task.ID})
	})
	t.Run("busy session refuses and writes nothing", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		task, r := archWorker(t, d, orch.ID, "w", model.StatusComplete)
		run := &fakeArchiveRunner{alive: true, idle: false}
		res := ArchiveRole(d, run, gitOK, r, false)
		testutil.Equal(t, res.Archived, false)
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Archived, false)
		testutil.Equal(t, len(run.stopped), 0)
	})
	t.Run("parked at prompt refuses", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		_, r := archWorker(t, d, orch.ID, "w", model.StatusComplete)
		res := ArchiveRole(d, &fakeArchiveRunner{alive: true, idle: true, blocked: true}, gitOK, r, false)
		testutil.Equal(t, res.Archived, false)
	})
	t.Run("dry run writes nothing", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		task, r := archWorker(t, d, orch.ID, "w", model.StatusComplete)
		res := ArchiveRole(d, &fakeArchiveRunner{}, gitOK, r, true)
		testutil.Equal(t, res.Archived, true)
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Archived, false)
	})
	t.Run("already archived is idempotent", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		_, r := archWorker(t, d, orch.ID, "w", model.StatusComplete)
		first := ArchiveRole(d, nil, gitOK, r, false)
		testutil.Equal(t, first.Archived, true)
		r2, _ := d.HeraRole(r.ID)
		second := ArchiveRole(d, nil, gitOK, r2, false)
		testutil.Equal(t, second.Archived, true)
		testutil.Contains(t, second.Reason, "already")
	})
	t.Run("stop failure is reported but archive stands", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		task, r := archWorker(t, d, orch.ID, "w", model.StatusComplete)
		run := &fakeArchiveRunner{alive: true, idle: true, stopErr: errors.New("boom")}
		res := ArchiveRole(d, run, gitOK, r, false)
		testutil.Equal(t, res.Archived, true)
		testutil.Contains(t, res.Reason, "boom")
		got, _ := d.Get(task.ID)
		testutil.Equal(t, got.Archived, true)
	})
}

// subCoord builds parent orch + sub-coordinator worker role bridging a child orch.
func subCoord(t *testing.T, d *db.DB) (parentCoord, sub *db.HeraRole, childCoord *db.HeraRole, child *db.HeraOrchestrator) {
	t.Helper()
	parent, err := d.CreateHeraOrchestrator("parent", "master")
	testutil.NoError(t, err)
	pt := &model.Task{Name: "pc", Status: model.StatusInProgress, Project: "p"}
	testutil.NoError(t, d.Add(pt))
	parentCoord, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: parent.ID, Name: "coord", Kind: db.HeraKindCoordinator, ArgusProject: "p"}, pt.ID, "/wt/pc")
	testutil.NoError(t, err)
	_, sub = archWorker(t, d, parent.ID, "sub", model.StatusComplete)
	child, err = d.CreateHeraOrchestrator("child", "master")
	testutil.NoError(t, err)
	ct := &model.Task{Name: "cc", Status: model.StatusInProgress, Project: "p"}
	testutil.NoError(t, d.Add(ct))
	childCoord, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: child.ID, Name: "ccoord", Kind: db.HeraKindCoordinator, ArgusProject: "p"}, ct.ID, "/wt/cc")
	testutil.NoError(t, err)
	testutil.NoError(t, d.CreateHeraOrchLink(parent.ID, child.ID, sub.ID))
	return
}

func TestArchive_SubCoordinator(t *testing.T) {
	t.Run("open decision blocks", func(t *testing.T) {
		d := archDB(t)
		_, sub, cc, _ := subCoord(t, d)
		_, err := d.PostHeraBoardItem(cc.ID, "decision", "pick one", "body")
		testutil.NoError(t, err)
		v := ArchivableVerdict(d, gitOK, sub)
		testutil.Equal(t, v.Archivable, false)
		testutil.Contains(t, v.Reason, "decision")
	})
	t.Run("live child role blocks", func(t *testing.T) {
		d := archDB(t)
		_, sub, _, child := subCoord(t, d)
		archWorker(t, d, child.ID, "kid", model.StatusComplete)
		v := ArchivableVerdict(d, gitOK, sub)
		testutil.Contains(t, v.Reason, "kid")
	})
	t.Run("fyi pushed to parent coordinator board and resolved", func(t *testing.T) {
		d := archDB(t)
		pc, sub, cc, _ := subCoord(t, d)
		_, err := d.PostHeraBoardItem(cc.ID, "fyi", "heads up", "details")
		testutil.NoError(t, err)
		res := ArchiveRole(d, nil, gitOK, sub, false)
		testutil.Equal(t, res.Archived, true)
		up, err := d.ListHeraBoardItems(pc.ID, false)
		testutil.NoError(t, err)
		testutil.Equal(t, len(up), 1)
		testutil.Equal(t, up[0].Subject, "[sub] heads up")
		orig, err := d.ListHeraBoardItems(cc.ID, false)
		testutil.NoError(t, err)
		testutil.Equal(t, len(orig), 0)
	})
}

func TestArchivableVerdict_SharedTaskAndOutbox(t *testing.T) {
	d := archDB(t)
	orch, _ := d.CreateHeraOrchestrator("o", "master")
	other, _ := d.CreateHeraOrchestrator("o2", "master")

	t.Run("task also bound to another live role refuses", func(t *testing.T) {
		task, r := archWorker(t, d, orch.ID, "shared", model.StatusComplete)
		_, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{
			OrchestratorID: other.ID, Name: "shared-hat", Kind: db.HeraKindFreelance, ArgusProject: "p",
		}, task.ID, task.Worktree)
		testutil.NoError(t, err)
		v := ArchivableVerdict(d, gitOK, r)
		testutil.Equal(t, v.Archivable, false)
		testutil.Contains(t, v.Reason, "another live role")
	})
	t.Run("unread outbound mail refuses", func(t *testing.T) {
		_, r := archWorker(t, d, orch.ID, "sender", model.StatusComplete)
		_, coord := archWorker(t, d, orch.ID, "recipient", model.StatusInProgress)
		_, err := d.SendHeraMessage(r.ID, coord.ID, "done", "tldr", nil)
		testutil.NoError(t, err)
		v := ArchivableVerdict(d, gitOK, r)
		testutil.Equal(t, v.Archivable, false)
		testutil.Contains(t, v.Reason, "not yet read")
	})
}

// A retry after a partial failure (posted to the parent, original left
// unresolved) must not duplicate the fyi on the parent board.
func TestArchive_FyiPushIsIdempotent(t *testing.T) {
	d := archDB(t)
	pc, sub, cc, _ := subCoord(t, d)
	it, err := d.PostHeraBoardItem(cc.ID, "fyi", "heads up", "details")
	testutil.NoError(t, err)
	marker := fmt.Sprintf("— moved from sub fyi #%d", it.ID)
	_, err = d.PostHeraBoardItem(pc.ID, "fyi", "[sub] heads up", "details\n\n"+marker)
	testutil.NoError(t, err)

	testutil.Equal(t, ArchiveRole(d, nil, gitOK, sub, false).Archived, true)
	up, err := d.ListHeraBoardItems(pc.ID, true)
	testutil.NoError(t, err)
	testutil.Equal(t, len(up), 1) // no duplicate
	left, err := d.ListHeraBoardItems(cc.ID, false)
	testutil.NoError(t, err)
	testutil.Equal(t, len(left), 0) // original now resolved
}

func TestArchive_FyiSubjectRuneSafe(t *testing.T) {
	d := archDB(t)
	pc, sub, cc, _ := subCoord(t, d)
	long := strings.Repeat("é", 120) // 2 bytes each; a byte cut at 240 could split a rune
	_, err := d.PostHeraBoardItem(cc.ID, "fyi", long, "details")
	testutil.NoError(t, err)
	testutil.Equal(t, ArchiveRole(d, nil, gitOK, sub, false).Archived, true)
	up, err := d.ListHeraBoardItems(pc.ID, true)
	testutil.NoError(t, err)
	testutil.Equal(t, len(up), 1)
	if !utf8.ValidString(up[0].Subject) || len(up[0].Subject) > 240 {
		t.Fatalf("subject not rune-safe/bounded: %d bytes", len(up[0].Subject))
	}
}

// The session turning busy between the idle check and commit rolls the whole
// archive back (idleness is the PreCommit hook): nothing is archived, no stop.
func TestArchiveRole_SessionTurnsBusyBeforeCommitRollsBack(t *testing.T) {
	d := archDB(t)
	orch, _ := d.CreateHeraOrchestrator("o", "master")
	task, r := archWorker(t, d, orch.ID, "w", model.StatusComplete)
	run := &flipRunner{fakeArchiveRunner: fakeArchiveRunner{alive: true, idle: true}}
	res := ArchiveRole(d, run, gitOK, r, false)
	testutil.Equal(t, res.Archived, false)
	testutil.Contains(t, res.Reason, "became busy") // PreCommit refusal, not the earlier pre-check
	testutil.Equal(t, len(run.stopped), 0)
	got, _ := d.Get(task.ID)
	testutil.Equal(t, got.Archived, false)
	role, _ := d.HeraRole(r.ID)
	testutil.Equal(t, role.ArchivedAt == nil, true)
}

// flipRunner reports idle on the first IsIdle call (predicate) and busy after.
type flipRunner struct {
	fakeArchiveRunner
	calls int
}

func (f *flipRunner) IsIdle(string) bool { f.calls++; return f.calls == 1 }

// failStore forces individual lookups to error so each fail-closed branch is
// pinned: deleting a clause's error handling must fail a test.
type failStore struct {
	*db.DB
	statusErr, inboxErr, outboxErr, blocksErr, linksErr, residentErr, bindingsErr bool
}

var errBoom = errors.New("boom")

func (f *failStore) HeraRoleStatusFor(id int64) (*db.HeraRoleStatus, error) {
	if f.statusErr {
		return nil, errBoom
	}
	return f.DB.HeraRoleStatusFor(id)
}
func (f *failStore) HeraArchiveUnread(id int64) (int, error) {
	if f.inboxErr {
		return 0, errBoom
	}
	return f.DB.HeraArchiveUnread(id)
}
func (f *failStore) HeraUnreadFromRole(id int64) (int, error) {
	if f.outboxErr {
		return 0, errBoom
	}
	return f.DB.HeraUnreadFromRole(id)
}
func (f *failStore) ListHeraBlocks(id int64) ([]db.HeraBlock, error) {
	if f.blocksErr {
		return nil, errBoom
	}
	return f.DB.ListHeraBlocks(id)
}
func (f *failStore) ListHeraOrchLinks() ([]db.HeraOrchLink, error) {
	if f.linksErr {
		return nil, errBoom
	}
	return f.DB.ListHeraOrchLinks()
}
func (f *failStore) IsResidentWorker(id string) (bool, error) {
	if f.residentErr {
		return false, errBoom
	}
	return f.DB.IsResidentWorker(id)
}
func (f *failStore) ListHeraLiveBindingsByTask(id string) ([]*db.HeraBinding, error) {
	if f.bindingsErr {
		return nil, errBoom
	}
	return f.DB.ListHeraLiveBindingsByTask(id)
}

func TestArchivableVerdict_FailClosedOnLookupErrors(t *testing.T) {
	cases := map[string]struct {
		set  func(*failStore)
		want string
	}{
		"status":   {func(f *failStore) { f.statusErr = true }, "status unreadable"},
		"outbox":   {func(f *failStore) { f.outboxErr = true }, "outbox unreadable"},
		"blocks":   {func(f *failStore) { f.blocksErr = true }, "plan edges unreadable"},
		"links":    {func(f *failStore) { f.linksErr = true }, "links unreadable"},
		"resident": {func(f *failStore) { f.residentErr = true }, "unverifiable"},
		"bindings": {func(f *failStore) { f.bindingsErr = true }, "bindings unreadable"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			d := archDB(t)
			orch, _ := d.CreateHeraOrchestrator("o", "master")
			_, r := archWorker(t, d, orch.ID, "w", model.StatusComplete)
			fs := &failStore{DB: d}
			tc.set(fs)
			v := ArchivableVerdict(fs, gitOK, r)
			testutil.Equal(t, v.Archivable, false)
			testutil.Contains(t, v.Reason, tc.want)
		})
	}
}

func TestArchivableVerdict_ProtectedAndMail(t *testing.T) {
	t.Run("pinned role", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		_, r := archWorker(t, d, orch.ID, "w", model.StatusComplete)
		testutil.NoError(t, d.PinHeraRole(r.ID))
		r2, _ := d.HeraRole(r.ID)
		testutil.Contains(t, ArchivableVerdict(d, gitOK, r2).Reason, "pinned")
	})
	t.Run("pinned task", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		task, r := archWorker(t, d, orch.ID, "w", model.StatusComplete)
		task.Pinned = true
		testutil.NoError(t, d.Update(task))
		testutil.Contains(t, ArchivableVerdict(d, gitOK, r).Reason, "pinned")
	})
	t.Run("resident worker marker", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		task, r := archWorker(t, d, orch.ID, "w", model.StatusComplete)
		testutil.NoError(t, d.SetMeta(task.ID, "hera", "resident_worker", "true"))
		testutil.Contains(t, ArchivableVerdict(d, gitOK, r).Reason, "standing-team")
	})
	t.Run("unread inbound mail is forwarded, not blocking", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		_, r := archWorker(t, d, orch.ID, "w", model.StatusComplete)
		_, c := archWorker(t, d, orch.ID, "c", model.StatusInProgress)
		_, err := d.SendHeraMessage(c.ID, r.ID, "ping", "tldr", nil)
		testutil.NoError(t, err)
		testutil.Equal(t, ArchivableVerdict(d, gitOK, r).Archivable, true)
	})
	t.Run("no branch or worktree is archivable with nil git check", func(t *testing.T) {
		d := archDB(t)
		orch, _ := d.CreateHeraOrchestrator("o", "master")
		task := &model.Task{Name: "bare", Status: model.StatusComplete, Project: "p"}
		testutil.NoError(t, d.Add(task))
		r, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "bare", Kind: db.HeraKindWorker, ArgusProject: "p"}, task.ID, "")
		testutil.NoError(t, err)
		testutil.NoError(t, d.SetStatus(task.ID, model.StatusComplete))
		testutil.Equal(t, ArchivableVerdict(d, nil, r).Archivable, true)
	})
}

// Plan-DAG kind=subcoord shape: ONE task carries the parent worker hat and a
// child-orchestrator coordinator hat; NO hera_orch_links row is written.
func materializedSubCoord(t *testing.T, d *db.DB) (pc, sub, cc *db.HeraRole, child *db.HeraOrchestrator, task *model.Task) {
	t.Helper()
	parent, err := d.CreateHeraOrchestrator("parent", "master")
	testutil.NoError(t, err)
	pt := &model.Task{Name: "pc", Status: model.StatusInProgress, Project: "p"}
	testutil.NoError(t, d.Add(pt))
	pc, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: parent.ID, Name: "coord", Kind: db.HeraKindCoordinator, ArgusProject: "p"}, pt.ID, "/wt/pc")
	testutil.NoError(t, err)
	task, sub = archWorker(t, d, parent.ID, "sub", model.StatusComplete)
	child, err = d.CreateHeraOrchestrator("child", "")
	testutil.NoError(t, err)
	cc, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: child.ID, Name: "coord", Kind: db.HeraKindCoordinator, ArgusProject: "p"}, task.ID, task.Worktree)
	testutil.NoError(t, err)
	return
}

func TestArchive_MaterializedSubCoordinator(t *testing.T) {
	t.Run("live child role blocks despite no link row", func(t *testing.T) {
		d := archDB(t)
		_, sub, _, child, _ := materializedSubCoord(t, d)
		archWorker(t, d, child.ID, "kid", model.StatusComplete)
		v := ArchivableVerdict(d, gitOK, sub)
		testutil.Equal(t, v.Archivable, false)
		testutil.Contains(t, v.Reason, "kid")
	})
	t.Run("open decision blocks", func(t *testing.T) {
		d := archDB(t)
		_, sub, cc, _, _ := materializedSubCoord(t, d)
		_, err := d.PostHeraBoardItem(cc.ID, "decision", "pick", "b")
		testutil.NoError(t, err)
		testutil.Contains(t, ArchivableVerdict(d, gitOK, sub).Reason, "decision")
	})
	t.Run("clean sub-coordinator archives, pushes fyi, ends child hat", func(t *testing.T) {
		d := archDB(t)
		pc, sub, cc, _, task := materializedSubCoord(t, d)
		_, err := d.PostHeraBoardItem(cc.ID, "fyi", "note", "body")
		testutil.NoError(t, err)
		res := ArchiveRole(d, nil, gitOK, sub, false)
		testutil.Equal(t, res.Archived, true)
		up, _ := d.ListHeraBoardItems(pc.ID, false)
		testutil.Equal(t, len(up), 1)
		testutil.Equal(t, up[0].Kind, "fyi")
		live, _ := d.ListHeraLiveBindingsByTask(task.ID)
		testutil.Equal(t, len(live), 1) // only the sub's own worker hat; coordinator hat ended
		testutil.Equal(t, live[0].RoleID, sub.ID)
	})
	t.Run("dry run touches nothing", func(t *testing.T) {
		d := archDB(t)
		pc, sub, cc, _, task := materializedSubCoord(t, d)
		_, err := d.PostHeraBoardItem(cc.ID, "fyi", "note", "body")
		testutil.NoError(t, err)
		res := ArchiveRole(d, nil, gitOK, sub, true)
		testutil.Equal(t, res.Archived, true)
		up, _ := d.ListHeraBoardItems(pc.ID, true)
		testutil.Equal(t, len(up), 0)
		left, _ := d.ListHeraBoardItems(cc.ID, false)
		testutil.Equal(t, len(left), 1)
		r, _ := d.HeraRole(sub.ID)
		testutil.Equal(t, r.ArchivedAt == nil, true)
		g, _ := d.Get(task.ID)
		testutil.Equal(t, g.Archived, false)
	})
}

func TestArchiveRole_DeadSessionNeverStopped_RealWorktreeKept(t *testing.T) {
	d := archDB(t)
	orch, _ := d.CreateHeraOrchestrator("o", "master")
	wt := t.TempDir()
	task := &model.Task{Name: "w", Status: model.StatusComplete, Project: "p", Worktree: wt, Branch: "b"}
	testutil.NoError(t, d.Add(task))
	r, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "w", Kind: db.HeraKindWorker, ArgusProject: "p"}, task.ID, wt)
	testutil.NoError(t, err)
	testutil.NoError(t, d.SetStatus(task.ID, model.StatusComplete))
	run := &fakeArchiveRunner{alive: false}
	testutil.Equal(t, ArchiveRole(d, run, gitOK, r, false).Archived, true)
	testutil.Equal(t, len(run.stopped), 0)
	if _, err := os.Stat(wt); err != nil {
		t.Fatalf("worktree must be preserved: %v", err)
	}
}

func TestArchivableVerdict_OutboxIgnoresDeadRecipients(t *testing.T) {
	d := archDB(t)
	orch, _ := d.CreateHeraOrchestrator("o", "master")
	_, r := archWorker(t, d, orch.ID, "sender", model.StatusComplete)
	_, gone := archWorker(t, d, orch.ID, "gone", model.StatusInProgress)
	_, err := d.SendHeraMessage(r.ID, gone.ID, "hi", "tldr", nil)
	testutil.NoError(t, err)
	testutil.Contains(t, ArchivableVerdict(d, gitOK, r).Reason, "not yet read")
	testutil.NoError(t, d.ArchiveHeraRole(gone.ID)) // recipient will never read it
	testutil.Equal(t, ArchivableVerdict(d, gitOK, r).Archivable, true)
}

func TestArchive_FyiHugeBodyStillMoves(t *testing.T) {
	d := archDB(t)
	pc, sub, cc, _ := subCoord(t, d)
	_, err := d.PostHeraBoardItem(cc.ID, "fyi", "big", strings.Repeat("x", 64*1024))
	testutil.NoError(t, err)
	testutil.Equal(t, ArchiveRole(d, nil, gitOK, sub, false).Archived, true)
	up, _ := d.ListHeraBoardItems(pc.ID, false)
	testutil.Equal(t, len(up), 1)
	testutil.Equal(t, len(up[0].Body) <= 64*1024, true)
}

// A task that only once coordinated an unrelated (ended, non-accepted) orchestrator
// is not a bridge.
func TestArchivableVerdict_EndedUnrelatedCoordinatorIsNotBridge(t *testing.T) {
	d := archDB(t)
	orch, _ := d.CreateHeraOrchestrator("o", "master")
	task, r := archWorker(t, d, orch.ID, "w", model.StatusComplete)
	other, _ := d.CreateHeraOrchestrator("other", "")
	_, b, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: other.ID, Name: "coord", Kind: db.HeraKindCoordinator, ArgusProject: "p"}, task.ID, task.Worktree)
	testutil.NoError(t, err)
	testutil.NoError(t, d.EndHeraBinding(b.ID, "user_deleted"))
	kids, err := childOrchs(d, r, task.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(kids), 0)
	testutil.Equal(t, ArchivableVerdict(d, gitOK, r).Archivable, true)
}

// --- Closing the three accepted gaps (PR #1223 follow-up) ---

// Gap 1: state that changes AFTER the predicate (e.g. during the slow git
// check) must still stop the archive.
func TestArchiveRole_StateChangeDuringGitCheckIsCaught(t *testing.T) {
	wantReason := map[string]string{"new live binding": "another live role", "new unread mail": "unread", "task reopened": "not complete"}
	cases := map[string]func(t *testing.T, d *db.DB, orchID int64, task *model.Task, r *db.HeraRole){
		"new live binding": func(t *testing.T, d *db.DB, _ int64, task *model.Task, _ *db.HeraRole) {
			o2, _ := d.CreateHeraOrchestrator("o2", "master")
			_, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: o2.ID, Name: "late", Kind: db.HeraKindFreelance, ArgusProject: "p"}, task.ID, task.Worktree)
			testutil.NoError(t, err)
		},
		"new unread mail": func(t *testing.T, d *db.DB, orchID int64, _ *model.Task, r *db.HeraRole) {
			_, c := archWorker(t, d, orchID, "c", model.StatusInProgress)
			_, err := d.SendHeraMessage(c.ID, r.ID, "late", "tldr", nil)
			testutil.NoError(t, err)
		},
		"task reopened": func(t *testing.T, d *db.DB, _ int64, task *model.Task, _ *db.HeraRole) {
			testutil.NoError(t, d.SetStatus(task.ID, model.StatusInProgress))
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			d := archDB(t)
			orch, _ := d.CreateHeraOrchestrator("o", "master")
			task, r := archWorker(t, d, orch.ID, "w", model.StatusComplete)
			calls := 0
			slowGit := func(*model.Task) (bool, string) {
				calls++
				if calls == 2 { // lands after the predicate passed, during the pre-tx re-check
					mutate(t, d, orch.ID, task, r)
				}
				return true, ""
			}
			res := ArchiveRole(d, nil, slowGit, r, false)
			testutil.Equal(t, res.Archived, false)
			// Reached the IN-TRANSACTION guard (not an earlier Go re-check).
			testutil.Contains(t, res.Reason, "changed during archive:")
			testutil.Contains(t, res.Reason, wantReason[name])
			got, _ := d.Get(task.ID)
			testutil.Equal(t, got.Archived, false)
		})
	}
}

// Gap 2: archiving a sub-coordinator also archives its child orchestrator.
func TestArchive_SubCoordinatorArchivesChildOrchestrator(t *testing.T) {
	for name, build := range map[string]func(*testing.T, *db.DB) (*db.HeraRole, *db.HeraOrchestrator){
		"plan-DAG shape (no link row)": func(t *testing.T, d *db.DB) (*db.HeraRole, *db.HeraOrchestrator) {
			_, sub, _, child, _ := materializedSubCoord(t, d)
			return sub, child
		},
		"explicit link": func(t *testing.T, d *db.DB) (*db.HeraRole, *db.HeraOrchestrator) {
			_, sub, _, child := subCoord(t, d)
			return sub, child
		},
	} {
		t.Run(name, func(t *testing.T) {
			d := archDB(t)
			sub, child := build(t, d)
			res := ArchiveRole(d, nil, gitOK, sub, false)
			testutil.Equal(t, res.Archived, true)
			o, err := d.HeraOrchestrator(child.ID)
			testutil.NoError(t, err)
			testutil.Equal(t, o.ArchivedAt != nil, true)
		})
	}
}

// BlockedOnPrompt flipping inside the PreCommit hook also rolls back.
type promptFlipRunner struct {
	fakeArchiveRunner
	calls int
}

func (p *promptFlipRunner) BlockedOnPrompt(string) bool { p.calls++; return p.calls >= 2 }

func TestArchiveRole_PromptParkedBeforeCommitRollsBack(t *testing.T) {
	d := archDB(t)
	orch, _ := d.CreateHeraOrchestrator("o", "master")
	task, r := archWorker(t, d, orch.ID, "w", model.StatusComplete)
	run := &promptFlipRunner{fakeArchiveRunner: fakeArchiveRunner{alive: true, idle: true}}
	res := ArchiveRole(d, run, gitOK, r, false)
	testutil.Equal(t, res.Archived, false)
	testutil.Contains(t, res.Reason, "became busy")
	got, _ := d.Get(task.ID)
	testutil.Equal(t, got.Archived, false)
}

// Stop runs only AFTER the archive is committed (so an exit hook cannot race a
// non-complete status onto an archive in flight).
type orderRunner struct {
	fakeArchiveRunner
	d                                      *db.DB
	taskID                                 string
	roleID                                 int64
	sawArchivedAtStop, sawRoleHiddenAtStop bool
}

func (o *orderRunner) Stop(id string) error {
	t, _ := o.d.Get(id)
	r, _ := o.d.HeraRole(o.roleID)
	o.sawArchivedAtStop = t != nil && t.Archived
	o.sawRoleHiddenAtStop = r != nil && r.ArchivedAt != nil
	return nil
}

func TestArchiveRole_StopRunsAfterCommit(t *testing.T) {
	d := archDB(t)
	orch, _ := d.CreateHeraOrchestrator("o", "master")
	task, r := archWorker(t, d, orch.ID, "w", model.StatusComplete)
	run := &orderRunner{fakeArchiveRunner: fakeArchiveRunner{alive: true, idle: true}, d: d, taskID: task.ID, roleID: r.ID}
	res := ArchiveRole(d, run, gitOK, r, false)
	testutil.Equal(t, res.Archived, true)
	testutil.Equal(t, run.sawArchivedAtStop, true)
	testutil.Equal(t, run.sawRoleHiddenAtStop, true)
}

func TestArchiveRoleStaleMergeNotice(t *testing.T) {
	for _, dry := range []bool{true, false} {
		t.Run(fmt.Sprint(dry), func(t *testing.T) {
			d := archDB(t)
			o, err := d.CreateHeraOrchestrator("o", "")
			testutil.NoError(t, err)
			_, r := archWorker(t, d, o.ID, "worker", model.StatusInReview)
			_, c := archWorker(t, d, o.ID, "sender", model.StatusInProgress)
			binding, err := d.HeraLiveBindingByRole(r.ID)
			testutil.NoError(t, err)
			msg, err := d.SendHeraMergeNotice(binding.ArgusTaskID, c.ID, r.ID, "merged", "notice")
			testutil.NoError(t, err)
			testutil.NoError(t, d.SetStatus(binding.ArgusTaskID, model.StatusComplete))
			result := ArchiveRole(d, nil, gitOK, r, dry)
			testutil.Equal(t, result.Archived, true)
			msgs, err := d.HeraMessagesByIDs([]int64{msg.ID})
			testutil.NoError(t, err)
			testutil.Equal(t, msgs[0].ReadAt == nil, dry)
		})
	}
}
