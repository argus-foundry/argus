package hera

import (
	"errors"
	"strings"
	"testing"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/db"
	"github.com/argus-foundry/argus/internal/lastexchange"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/testutil"
)

// keep-coordinator-orientation-on-recycle: a coordinator's standing
// orientation is derived from its live structure at recycle time and carried
// into every recycle seed.

func openOrientationDB(t *testing.T) *db.DB {
	t.Helper()
	d, err := db.OpenInMemory()
	testutil.NoError(t, err)
	t.Cleanup(func() { _ = d.Close() })
	return d
}

// seedBridgedSubCoord builds the bridge shape: a task holding a worker binding
// on parentNodeName (with nodeKind) in orchestrator "parent" (coordinated by
// "boss") AND a coordinator binding ("coord") in orchestrator childOrch.
func seedBridgedSubCoord(t *testing.T, d *db.DB, nodeKind db.HeraNodeKind, childOrch string) (*model.Task, *db.HeraRole) {
	t.Helper()
	parent, err := d.CreateHeraOrchestrator("parent", "master")
	testutil.NoError(t, err)
	boss := &model.Task{Name: "boss-task", Status: model.StatusInProgress, Project: "p", Worktree: "/wt/boss"}
	testutil.NoError(t, d.Add(boss))
	_, _, err = d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{
		OrchestratorID: parent.ID, Name: "boss", Kind: db.HeraKindCoordinator, ArgusProject: "p",
	}, boss.ID, boss.Worktree)
	testutil.NoError(t, err)

	task := &model.Task{Name: "sub-task", Status: model.StatusInProgress, Project: "p", Worktree: "/wt/sub"}
	testutil.NoError(t, d.Add(task))
	node, err := d.CreateHeraRole(db.CreateHeraRoleInput{
		OrchestratorID: parent.ID, Name: "phase-two", Kind: db.HeraKindWorker, NodeKind: nodeKind,
		ArgusProject: "p", Prompt: "deliver phase two",
	})
	testutil.NoError(t, err)
	_, err = d.CreateHeraBinding(db.CreateHeraBindingInput{RoleID: node.ID, ArgusTaskID: task.ID, WorktreePath: task.Worktree})
	testutil.NoError(t, err)

	child, err := d.CreateHeraOrchestrator(childOrch, "")
	testutil.NoError(t, err)
	coord, _, err := d.CreateHeraRoleWithBinding(db.CreateHeraRoleInput{
		OrchestratorID: child.ID, Name: "coord", Kind: db.HeraKindCoordinator, ArgusProject: "p",
		Prompt: "deliver phase two",
	}, task.ID, task.Worktree)
	testutil.NoError(t, err)
	return task, coord
}

func TestCoordinatorOrientation_TopLevel(t *testing.T) {
	d := openOrientationDB(t)
	task, _, role := seedRecycleCoordinator(t, d, "rollout", "/wt/coord", "b", "mission")
	testutil.Equal(t, CoordinatorOrientation(d, role, task.ID), agent.HeraCoordinatorOrientation("rollout"))
}

func TestCoordinatorOrientation_SubCoordPlanNodeIncludesCheckIn(t *testing.T) {
	d := openOrientationDB(t)
	task, coord := seedBridgedSubCoord(t, d, db.HeraNodeKindSubCoord, "phase-two-2")
	got := CoordinatorOrientation(d, coord, task.ID)
	testutil.Contains(t, got, agent.HeraSubCoordinatorOrientation("phase-two-2", "parent", "coord"))
	testutil.Contains(t, got, agent.HeraCheckInOrientation("parent", "boss"))
	testutil.Contains(t, got, orientationCheckInDoneNote)
	if strings.Contains(got, agent.HeraCoordinatorOrientation("phase-two-2")) {
		t.Fatal("sub-coordinator must not get the top-level orientation")
	}
}

func TestCoordinatorOrientation_SelfPromotedWorkerHasNoCheckIn(t *testing.T) {
	d := openOrientationDB(t)
	task, coord := seedBridgedSubCoord(t, d, "", "promoted")
	got := CoordinatorOrientation(d, coord, task.ID)
	testutil.Equal(t, got, agent.HeraSubCoordinatorOrientation("promoted", "parent", "coord"))
}

func TestCoordinatorOrientation_NonCoordinatorIsEmpty(t *testing.T) {
	d := openOrientationDB(t)
	for _, kind := range []db.HeraRoleKind{db.HeraKindWorker, db.HeraKindFreelance} {
		t.Run(string(kind), func(t *testing.T) {
			task, _, role := seedRecycleRole(t, d, kind, "o-"+string(kind), string(kind), "/wt/"+string(kind), "b", "m")
			testutil.Equal(t, CoordinatorOrientation(d, role, task.ID), "")
		})
	}
}

// failingOrientationStore wraps a real DB and fails chosen lookups.
type failingOrientationStore struct {
	*db.DB
	failOrch, failBindings bool
}

func (f failingOrientationStore) HeraOrchestrator(id int64) (*db.HeraOrchestrator, error) {
	if f.failOrch {
		return nil, errors.New("orch unavailable")
	}
	return f.DB.HeraOrchestrator(id)
}

func (f failingOrientationStore) ListHeraLiveBindingsByTask(taskID string) ([]*db.HeraBinding, error) {
	if f.failBindings {
		return nil, errors.New("bindings unavailable")
	}
	return f.DB.ListHeraLiveBindingsByTask(taskID)
}

func TestCoordinatorOrientation_FailOpen(t *testing.T) {
	d := openOrientationDB(t)
	task, coord := seedBridgedSubCoord(t, d, db.HeraNodeKindSubCoord, "kid")
	t.Run("orchestrator lookup fails -> empty", func(t *testing.T) {
		testutil.Equal(t, CoordinatorOrientation(failingOrientationStore{DB: d, failOrch: true}, coord, task.ID), "")
	})
	t.Run("binding list fails -> top-level fallback", func(t *testing.T) {
		testutil.Equal(t, CoordinatorOrientation(failingOrientationStore{DB: d, failBindings: true}, coord, task.ID),
			agent.HeraCoordinatorOrientation("kid"))
	})
}

func TestBuildRecycleSeedPrompt_TopLevelCoordinatorCarriesOrientation(t *testing.T) {
	d := openOrientationDB(t)
	_, _, role := seedRecycleCoordinator(t, d, "rollout", "/wt/coord", "b", "MISSION-TEXT")
	prompt, err := BuildRecycleSeedPrompt(d, role.ID, "")
	testutil.NoError(t, err)

	orient := agent.HeraCoordinatorOrientation("rollout")
	testutil.Contains(t, prompt, orientationHeading)
	testutil.Contains(t, prompt, orient)
	headIdx := strings.Index(prompt, orientationHeading)
	origIdx := strings.Index(prompt, "## Original prompt")
	relayIdx := strings.Index(prompt, "## Open with the user")
	testutil.Equal(t, relayIdx < headIdx, true)
	testutil.Equal(t, headIdx < origIdx, true)
	testutil.Equal(t, strings.Index(prompt, orient) < origIdx, true)
}

// TestBuildRecycleSeedPrompt_OrientationSurvivesTwoRecycles mirrors the real
// restart path: each recycle overwrites task.Prompt with the seed, then the
// next recycle builds a fresh seed. The orientation must not depend on the
// task prompt.
func TestBuildRecycleSeedPrompt_OrientationSurvivesTwoRecycles(t *testing.T) {
	d := openOrientationDB(t)
	task, _, role := seedRecycleCoordinator(t, d, "rollout", "/wt/coord", "b", "MISSION-TEXT")
	orient := agent.HeraCoordinatorOrientation("rollout")
	for i := 0; i < 2; i++ {
		prompt, err := BuildRecycleSeedPrompt(d, role.ID, "")
		testutil.NoError(t, err)
		testutil.Equal(t, strings.Count(prompt, orient), 1)
		task.Prompt = prompt
		testutil.NoError(t, d.Update(task))
	}
}

func TestBuildRecycleSeedPrompt_SubCoordCarriesSubOrientationAndCheckIn(t *testing.T) {
	d := openOrientationDB(t)
	_, coord := seedBridgedSubCoord(t, d, db.HeraNodeKindSubCoord, "phase-two-2")
	prompt, err := BuildRecycleSeedPrompt(d, coord.ID, "")
	testutil.NoError(t, err)
	testutil.Contains(t, prompt, orientationHeading)
	testutil.Contains(t, prompt, agent.HeraSubCoordinatorOrientation("phase-two-2", "parent", "coord"))
	testutil.Contains(t, prompt, agent.HeraCheckInOrientation("parent", "boss"))
	testutil.Contains(t, prompt, orientationCheckInDoneNote)
	if strings.Contains(prompt, "five habits") {
		t.Fatal("sub-coordinator seed must not carry the top-level orientation")
	}
}

func TestBuildRecycleSeedPrompt_WorkerAndFreelanceCarryNoOrientation(t *testing.T) {
	d := openOrientationDB(t)
	for _, kind := range []db.HeraRoleKind{db.HeraKindWorker, db.HeraKindFreelance} {
		t.Run(string(kind), func(t *testing.T) {
			_, _, role := seedRecycleRole(t, d, kind, "o-"+string(kind), string(kind), "/wt/"+string(kind), "b", "mission")
			prompt, err := BuildRecycleSeedPrompt(d, role.ID, "")
			testutil.NoError(t, err)
			if strings.Contains(prompt, orientationHeading) || strings.Contains(prompt, "You are the coordinator") {
				t.Fatalf("%s seed carries coordinator orientation: %q", kind, prompt)
			}
		})
	}
}

// transcriptLine renders a Claude JSONL user/assistant line with plain-string
// content.
func transcriptLine(kind, text string) string {
	q := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`).Replace(text)
	return `{"type":"` + kind + `","message":{"role":"` + kind + `","content":"` + q + `"}}` + "\n"
}

func TestBuildRecycleSeedPrompt_MarkedAsArgusInjected(t *testing.T) {
	d := openOrientationDB(t)
	for _, kind := range []db.HeraRoleKind{db.HeraKindCoordinator, db.HeraKindWorker} {
		t.Run(string(kind), func(t *testing.T) {
			_, _, role := seedRecycleRole(t, d, kind, "m-"+string(kind), string(kind), "/wt/m-"+string(kind), "b", "mission")
			seed, err := BuildRecycleSeedPrompt(d, role.ID, "")
			testutil.NoError(t, err)
			testutil.Equal(t, strings.HasPrefix(seed, lastexchange.SystemTurnPrefix), true)

			// Second recycle with no human turn: the seed is not "the user".
			ex, err := lastexchange.Extract(strings.NewReader(
				transcriptLine("user", seed) + transcriptLine("assistant", "resuming work")))
			testutil.NoError(t, err)
			testutil.Equal(t, ex.Human, "")
			testutil.Equal(t, ex.Section(), "")

			// A human reply after the seed is still extracted.
			ex, err = lastexchange.Extract(strings.NewReader(
				transcriptLine("user", seed) + transcriptLine("assistant", "resuming") +
					transcriptLine("user", "what is the status?") + transcriptLine("assistant", "all green")))
			testutil.NoError(t, err)
			testutil.Equal(t, ex.Human, "what is the status?")
		})
	}
}

// TestBuildRecycleSeedPrompt_OrientationFailureNeverBlocksSeed pins the
// fail-open scenario end-to-end: an unresolvable orchestrator omits the
// orientation section but still yields a usable seed.
func TestBuildRecycleSeedPrompt_OrientationFailureNeverBlocksSeed(t *testing.T) {
	d := openOrientationDB(t)
	_, coord := seedBridgedSubCoord(t, d, db.HeraNodeKindSubCoord, "kid")
	prompt, err := BuildRecycleSeedPrompt(failingOrientationStore{DB: d, failOrch: true}, coord.ID, "")
	testutil.NoError(t, err)
	if strings.Contains(prompt, orientationHeading) {
		t.Fatal("orientation section must be omitted when the orchestrator lookup fails")
	}
	testutil.Contains(t, prompt, "## Original prompt")
	testutil.Contains(t, prompt, "deliver phase two")
}
