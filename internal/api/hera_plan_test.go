package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/drn/argus/internal/db"
	"github.com/drn/argus/internal/events"
	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
)

func getPlan(t *testing.T, srv *Server, path string) (int, heraPlanJSON) {
	t.Helper()
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("GET", path, ""))
	var resp heraPlanJSON
	if w.Code == http.StatusOK {
		testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	}
	return w.Code, resp
}

func planPath(orchID int64) string {
	return "/api/hera/orchestrators/" + strconv.FormatInt(orchID, 10) + "/plan"
}

func mkPlanned(t *testing.T, d *db.DB, orch int64, name string) *db.HeraRole {
	t.Helper()
	r, err := d.CreateHeraPlannedRole(db.CreateHeraRoleInput{OrchestratorID: orch, Name: name, Prompt: "do " + name, Archetype: "code_slice"})
	testutil.NoError(t, err)
	return r
}

func mkBound(t *testing.T, d *db.DB, orch int64, name string, status model.Status, result string) (*db.HeraRole, *model.Task) {
	t.Helper()
	task := &model.Task{Name: name, Project: "p", Status: status, Result: result}
	testutil.NoError(t, d.Add(task))
	r, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: orch, Name: name, Kind: db.HeraKindWorker}, task.ID, "/tmp/"+name)
	testutil.NoError(t, err)
	return r, task
}

func TestHeraPlan_StatesAndEdges(t *testing.T) {
	srv, d := testServer(t)
	orch, _ := d.CreateHeraOrchestrator("o", "")
	planned := mkPlanned(t, d, orch.ID, "planned")
	cancelled := mkPlanned(t, d, orch.ID, "cancelled")
	testutil.NoError(t, d.CancelHeraPlannedNode(cancelled.ID))
	failed, _ := mkBound(t, d, orch.ID, "failed", model.StatusInReview, `{"failed":true}`)
	review, _ := mkBound(t, d, orch.ID, "review", model.StatusInReview, "")
	accepted, _ := mkBound(t, d, orch.ID, "accepted", model.StatusComplete, "")
	pending, _ := mkBound(t, d, orch.ID, "pending", model.StatusPending, "")
	working, _ := mkBound(t, d, orch.ID, "working", model.StatusInProgress, "")
	_, err := d.CreateHeraRole(db.CreateHeraRoleInput{OrchestratorID: orch.ID, Name: "coord", Kind: db.HeraKindCoordinator})
	testutil.NoError(t, err)
	archived := mkPlanned(t, d, orch.ID, "archived")
	testutil.NoError(t, d.AddHeraBlock(archived.ID, planned.ID))
	testutil.NoError(t, d.ArchiveHeraRole(archived.ID))
	testutil.NoError(t, d.AddHeraBlock(review.ID, planned.ID))
	testutil.NoError(t, d.AddHeraBlock(accepted.ID, review.ID))

	code, resp := getPlan(t, srv, planPath(orch.ID))
	testutil.Equal(t, code, http.StatusOK)
	testutil.Equal(t, resp.OrchestratorID, orch.ID)
	testutil.True(t, resp.Authored)

	state := map[string]heraPlanNodeJSON{}
	for _, n := range resp.Nodes {
		state[n.Name] = n
	}
	testutil.Equal(t, len(resp.Nodes), 7)
	for name, want := range map[string]string{
		"planned": "planned", "cancelled": "cancelled", "failed": "failed",
		"review": "in_review", "accepted": "done", "pending": "pending", "working": "in_progress",
	} {
		testutil.Equal(t, state[name].State, want)
	}
	testutil.True(t, state["cancelled"].Cancelled)
	testutil.True(t, state["planned"].Planned)
	testutil.Nil(t, state["planned"].TaskID)
	testutil.Equal(t, *state["review"].TaskID != "", true)
	testutil.Equal(t, state["planned"].ID, "role:"+strconv.FormatInt(planned.ID, 10))
	testutil.Equal(t, state["planned"].Prompt, "do planned")
	testutil.Equal(t, state["planned"].Archetype, "code_slice")

	testutil.Equal(t, len(resp.Edges), 2)
	want := map[heraPlanEdgeJSON]bool{
		{BlockerID: "role:" + strconv.FormatInt(planned.ID, 10), DependentID: "role:" + strconv.FormatInt(review.ID, 10)}:  true,
		{BlockerID: "role:" + strconv.FormatInt(review.ID, 10), DependentID: "role:" + strconv.FormatInt(accepted.ID, 10)}: true,
	}
	for _, e := range resp.Edges {
		testutil.True(t, want[e])
	}
	_, _, _ = failed, pending, working
}

func TestHeraPlan_UnauthoredIsEmpty(t *testing.T) {
	srv, d := testServer(t)
	orch, _ := d.CreateHeraOrchestrator("o", "")
	mkBound(t, d, orch.ID, "live", model.StatusInProgress, "")
	code, resp := getPlan(t, srv, planPath(orch.ID))
	testutil.Equal(t, code, http.StatusOK)
	testutil.Equal(t, resp.Authored, false)
	testutil.Equal(t, len(resp.Nodes), 0)
	testutil.Equal(t, len(resp.Edges), 0)

	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, authedReq("GET", planPath(orch.ID), ""))
	var raw map[string]json.RawMessage
	testutil.NoError(t, json.Unmarshal(w.Body.Bytes(), &raw))
	testutil.Equal(t, string(raw["nodes"]), "[]")
	testutil.Equal(t, string(raw["edges"]), "[]")
}

func TestHeraPlan_WireIDStableAcrossMaterialization(t *testing.T) {
	srv, d := testServer(t)
	orch, _ := d.CreateHeraOrchestrator("o", "")
	a := mkPlanned(t, d, orch.ID, "a")
	b := mkPlanned(t, d, orch.ID, "b")
	testutil.NoError(t, d.AddHeraBlock(b.ID, a.ID))
	_, before := getPlan(t, srv, planPath(orch.ID))

	task := &model.Task{Name: "a", Project: "p", Status: model.StatusInProgress}
	testutil.NoError(t, d.Add(task))
	_, err := d.CreateHeraBinding(db.CreateHeraBindingInput{RoleID: a.ID, ArgusTaskID: task.ID, WorktreePath: "/tmp/a"})
	testutil.NoError(t, err)
	_, after := getPlan(t, srv, planPath(orch.ID))

	idOf := func(p heraPlanJSON, name string) heraPlanNodeJSON {
		for _, n := range p.Nodes {
			if n.Name == name {
				return n
			}
		}
		t.Fatalf("node %s missing", name)
		return heraPlanNodeJSON{}
	}
	testutil.Equal(t, idOf(before, "a").ID, idOf(after, "a").ID)
	testutil.Nil(t, idOf(before, "a").TaskID)
	testutil.Equal(t, *idOf(after, "a").TaskID, task.ID)
	testutil.Equal(t, idOf(after, "a").State, "in_progress")
	testutil.Equal(t, len(after.Edges), 1)
}

func TestHeraPlan_OrchestratorsDoNotCrossWire(t *testing.T) {
	srv, d := testServer(t)
	o1, _ := d.CreateHeraOrchestrator("o1", "")
	o2, _ := d.CreateHeraOrchestrator("o2", "")
	a1 := mkPlanned(t, d, o1.ID, "a")
	b1 := mkPlanned(t, d, o1.ID, "b")
	a2 := mkPlanned(t, d, o2.ID, "a")
	testutil.NoError(t, d.AddHeraBlock(b1.ID, a1.ID))
	if err := d.AddHeraBlock(a2.ID, a1.ID); err == nil {
		t.Fatal("cross-orchestrator edge must be rejected")
	}
	_, p2 := getPlan(t, srv, planPath(o2.ID))
	testutil.Equal(t, len(p2.Edges), 0)
	testutil.Equal(t, len(p2.Nodes), 1)
	_, p1 := getPlan(t, srv, planPath(o1.ID))
	testutil.Equal(t, len(p1.Edges), 1)
	testutil.Equal(t, len(p1.Nodes), 2)
}

func TestHeraPlan_NestedChildOrchestrator(t *testing.T) {
	srv, d := testServer(t)
	parent, _ := d.CreateHeraOrchestrator("parent", "")
	child, _ := d.CreateHeraOrchestrator("child", "")
	mkPlanned(t, d, parent.ID, "later")
	sub, subTask := mkBound(t, d, parent.ID, "sub", model.StatusInProgress, "")
	_, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{OrchestratorID: child.ID, Name: "coord", Kind: db.HeraKindCoordinator}, subTask.ID, "/tmp/sub")
	testutil.NoError(t, err)

	testutil.NoError(t, d.CreateHeraOrchLink(parent.ID, child.ID, sub.ID))
	_, resp := getPlan(t, srv, planPath(parent.ID))
	var found *heraPlanNodeJSON
	for i := range resp.Nodes {
		if resp.Nodes[i].RoleID == sub.ID {
			found = &resp.Nodes[i]
		}
	}
	testutil.True(t, found != nil)
	testutil.True(t, found.ChildOrchestratorID != nil)
	testutil.Equal(t, *found.ChildOrchestratorID, child.ID)
	for _, n := range resp.Nodes {
		if n.RoleID != sub.ID {
			testutil.Nil(t, n.ChildOrchestratorID)
		}
	}
}

func TestHeraPlan_Errors(t *testing.T) {
	srv, d := testServer(t)
	for _, p := range []string{"/api/hera/orchestrators/abc/plan", "/api/hera/orchestrators/0/plan", "/api/hera/orchestrators/-3/plan"} {
		code, _ := getPlan(t, srv, p)
		testutil.Equal(t, code, http.StatusBadRequest)
	}
	code, _ := getPlan(t, srv, planPath(9999))
	testutil.Equal(t, code, http.StatusNotFound)

	w := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/hera/orchestrators/1/plan", nil)
	authMiddleware("test-token", nil, nil, srv.routes(), "/public").ServeHTTP(w, req)
	testutil.Equal(t, w.Code, http.StatusUnauthorized)

	orch, _ := d.CreateHeraOrchestrator("big", "")
	specs := make([]db.HeraPlannedNodeSpec, maxPlanNodes+1)
	for i := range specs {
		specs[i] = db.HeraPlannedNodeSpec{Name: "n" + strconv.Itoa(i)}
	}
	_, err := d.CreateHeraPlan(orch.ID, specs, nil)
	testutil.NoError(t, err)
	code, _ = getPlan(t, srv, planPath(orch.ID))
	testutil.Equal(t, code, http.StatusRequestEntityTooLarge)
}

func TestHeraPlan_EdgeLimit(t *testing.T) {
	srv, d := testServer(t)
	orch, _ := d.CreateHeraOrchestrator("edges", "")
	const n = 101
	specs := make([]db.HeraPlannedNodeSpec, n)
	for i := range specs {
		specs[i] = db.HeraPlannedNodeSpec{Name: "n" + strconv.Itoa(i)}
	}
	var edges []db.HeraBlockSpec
	for i := 0; i < n && len(edges) <= maxPlanEdges; i++ {
		for j := i + 1; j < n && len(edges) <= maxPlanEdges; j++ {
			edges = append(edges, db.HeraBlockSpec{BlockedNodeIdx: j, BlockerNodeIdx: i})
		}
	}
	testutil.Equal(t, len(edges), maxPlanEdges+1)
	_, err := d.CreateHeraPlan(orch.ID, specs, edges)
	testutil.NoError(t, err)
	code, _ := getPlan(t, srv, planPath(orch.ID))
	testutil.Equal(t, code, http.StatusRequestEntityTooLarge)
}

func TestHeraPlan_GetDoesNotMutate(t *testing.T) {
	srv, d := testServer(t)
	orch, _ := d.CreateHeraOrchestrator("o", "")
	a := mkPlanned(t, d, orch.ID, "a")
	b := mkPlanned(t, d, orch.ID, "b")
	testutil.NoError(t, d.AddHeraBlock(b.ID, a.ID))
	sink := &planEventSink{}
	restore := setPlanSink(sink)
	defer restore()
	_, r1 := getPlan(t, srv, planPath(orch.ID))
	_, r2 := getPlan(t, srv, planPath(orch.ID))
	testutil.DeepEqual(t, r1, r2)
	testutil.Equal(t, len(sink.got), 0)
	blocks, err := d.ListHeraBlocks(orch.ID)
	testutil.NoError(t, err)
	testutil.Equal(t, len(blocks), 1)
}

type planEventSink struct{ got []model.Event }

func (s *planEventSink) Emit(ev model.Event) {
	if ev.Type == model.EventTypeHeraPlanChanged {
		s.got = append(s.got, ev)
	}
}

func setPlanSink(s *planEventSink) func() {
	prev := events.SetSink(s)
	return func() { events.SetSink(prev) }
}

func TestHeraPlan_MutationEmitsPayload(t *testing.T) {
	_, d := testServer(t)
	orch, _ := d.CreateHeraOrchestrator("o", "")
	sink := &planEventSink{}
	defer setPlanSink(sink)()
	mkPlanned(t, d, orch.ID, "a")
	testutil.Equal(t, len(sink.got), 1)
	testutil.Equal(t, string(sink.got[0].Payload), `{"orchestrator_id":`+strconv.FormatInt(orch.ID, 10)+`}`)
}
