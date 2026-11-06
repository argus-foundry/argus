package taskview

import (
	"testing"

	"github.com/drn/argus/internal/model"
	"github.com/drn/argus/internal/testutil"
	"github.com/gdamore/tcell/v2"
)

func TestTaskRailFocusFoldAndArchive(t *testing.T) {
	tl := NewTaskListView()
	tl.SetRailMode(true)
	tl.SetTasks([]*model.Task{{ID: "a", Project: "A"}, {ID: "b", Project: "B"}, {ID: "c", Project: "A", Archived: true}, {ID: "p", Project: "P", Pinned: true}})
	testutil.DeepEqual(t, tl.VisibleTaskIDs(), []string{"p", "a"})
	tl.SelectByID("a")
	tl.CursorUp()
	testutil.Equal(t, tl.SelectedTask().ID, "p")
	tl.InputHandler()(tcell.NewEventKey(tcell.KeyRune, ' ', 0), nil)
	testutil.DeepEqual(t, tl.VisibleTaskIDs(), []string{"p", "a"})
	tl.CursorDown() // A task
	tl.CursorDown() // B task
	testutil.Equal(t, tl.SelectedTask().ID, "b")
	tl.SetTasks(tl.tasks)
	testutil.DeepEqual(t, tl.VisibleTaskIDs(), []string{"p", "b"})
	tl.CursorDown() // First archived task, skipping the section/project headers.
	testutil.Equal(t, tl.SelectedTask().ID, "c")
	testutil.DeepEqual(t, tl.VisibleTaskIDs(), []string{"p", "b", "c"})
	tl.SelectByID("c")
	tl.SelectByID("a")
	testutil.DeepEqual(t, tl.VisibleTaskIDs(), []string{"p", "a", "c"})
}

func TestTaskRailNavigationSkipsEveryHeader(t *testing.T) {
	for _, expandAll := range []bool{false, true} {
		t.Run(map[bool]string{false: "focus fold", true: "all open"}[expandAll], func(t *testing.T) {
			tl := NewTaskListView()
			tl.SetRailMode(true)
			tl.SetExpandAll(expandAll)
			tl.SetTasks([]*model.Task{{ID: "a", Project: "A"}, {ID: "b", Project: "B"}, {ID: "c", Project: "C"}, {ID: "old", Project: "D", Archived: true}})
			var notified []string
			tl.OnCursorChange = func(task *model.Task) {
				if task == nil {
					t.Fatal("navigation notified a header selection")
				}
				notified = append(notified, task.ID)
			}
			tl.SelectByID("a")
			tl.CursorUp()
			testutil.Equal(t, tl.SelectedTask().ID, "a")
			for _, id := range []string{"b", "c", "old", "old"} {
				tl.CursorDown()
				testutil.Equal(t, tl.SelectedTask().ID, id)
			}
			for _, id := range []string{"c", "b", "a", "a"} {
				tl.CursorUp()
				testutil.Equal(t, tl.SelectedTask().ID, id)
			}
			testutil.Equal(t, len(notified) > 0, true)
		})
	}
}

func TestTaskRailRestorePaths(t *testing.T) {
	cases := []struct {
		name string
		run  func(*TaskListView)
		want string
	}{
		{"startup", func(tl *TaskListView) { tl.SetExpanded("missing"); tl.SelectByID("missing") }, "a"},
		{"refresh", func(tl *TaskListView) { tl.SetTasks(tl.tasks) }, "a"},
		{"expand and fold", func(tl *TaskListView) { tl.SetExpandAll(true); tl.SetExpandAll(false) }, "a"},
		{"filter", func(tl *TaskListView) { tl.SetFilter("b"); tl.ClearFilter() }, "b"},
		{"hide managed", func(tl *TaskListView) { tl.SetManagedTasks(map[string]bool{"a": true}); tl.ToggleHeraManaged() }, "b"},
		{"delete", func(tl *TaskListView) { tl.SetTasks(tl.tasks[1:]) }, "b"},
		{"archive", func(tl *TaskListView) { tl.tasks[0].Archived = true; tl.SetTasks(tl.tasks) }, "b"},
		{"enable rail", func(tl *TaskListView) { tl.SetRailMode(false); tl.SetRailMode(true) }, "a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tl := NewTaskListView()
			tl.SetRailMode(true)
			tl.SetTasks([]*model.Task{{ID: "a", Name: "a", Project: "A"}, {ID: "b", Name: "b", Project: "B"}})
			tc.run(tl)
			if tl.SelectedTask() == nil {
				t.Fatalf("restore left no task selected: cursor=%d rows=%v", tl.cursor, tl.rows)
			}
			testutil.Equal(t, len(tl.VisibleTaskIDs()) > 0, true)
			testutil.Equal(t, tl.SelectedTask().ID, tc.want)
		})
	}
}

func TestTaskRailRestoreStaleHeader(t *testing.T) {
	for _, restore := range []string{"refresh", "expand", "fold", "enable"} {
		t.Run(restore, func(t *testing.T) {
			tl := NewTaskListView()
			tl.SetRailMode(true)
			tl.SetTasks([]*model.Task{{ID: "a", Project: "A"}})
			if restore == "fold" {
				tl.SetExpandAll(true)
			}
			if restore == "enable" {
				tl.SetRailMode(false)
			}
			tl.cursor = 0 // Stale header selection from the previous rail behavior.
			switch restore {
			case "refresh":
				tl.SetTasks(tl.tasks)
			case "expand":
				tl.SetExpandAll(true)
			case "fold":
				tl.SetExpandAll(false)
			case "enable":
				tl.SetRailMode(true)
			}
			if tl.SelectedTask() == nil {
				t.Fatal("restored a project header instead of its task")
			}
			testutil.Equal(t, tl.SelectedTask().ID, "a")
		})
	}
}

func TestTaskRailOnlyArchivedTasks(t *testing.T) {
	tl := NewTaskListView()
	tl.SetRailMode(true)
	tl.SetTasks([]*model.Task{{ID: "old", Project: "A", Archived: true}})
	if tl.SelectedTask() == nil {
		t.Fatal("archive-only rail has no selected task")
	}
	testutil.Equal(t, tl.SelectedTask().ID, "old")
	tl.SetTasks([]*model.Task{{ID: "other", Project: "B", Archived: true}})
	if tl.SelectedTask() == nil {
		t.Fatal("deleting the expanded archive project left no selected task")
	}
	testutil.Equal(t, tl.SelectedTask().ID, "other")
	tl.SetTasks(nil)
	testutil.Nil(t, tl.SelectedTask())
}

func TestTaskRailNotifiesWhenFoldKeepsCursorIndex(t *testing.T) {
	tl := NewTaskListView()
	tl.SetRailMode(true)
	tl.SetTasks([]*model.Task{{ID: "a1", Project: "A"}, {ID: "a2", Project: "A"}, {ID: "b1", Project: "B"}, {ID: "b2", Project: "B"}})
	tl.SelectByID("a2")
	var selected []string
	tl.OnCursorChange = func(task *model.Task) {
		if task != nil {
			selected = append(selected, task.ID)
		}
	}
	tl.CursorDown()
	testutil.Equal(t, tl.SelectedTask().ID, "b1")
	testutil.DeepEqual(t, selected, []string{"b1"})
	tl.CursorUp()
	testutil.Equal(t, tl.SelectedTask().ID, "a2")
	testutil.DeepEqual(t, selected, []string{"b1", "a2"})
}

func TestTaskRailFilterAndLegacyIsolation(t *testing.T) {
	tl := NewTaskListView()
	tasks := []*model.Task{{ID: "a", Project: "A", Name: "first"}, {ID: "b", Project: "B", Name: "worker"}}
	tl.SetTasks(tasks)
	tl.SelectByID("a")
	legacy := tl.VisibleTaskIDs()
	tl.SetRailMode(true)
	tl.SetManagedTasks(map[string]bool{"b": true})
	tl.ToggleHeraManaged()
	testutil.DeepEqual(t, tl.VisibleTaskIDs(), []string{"a"})
	testutil.Nil(t, tl.AdjacentTask("a", 1))
	tl.ToggleHeraManaged()
	tl.SetRailMode(false)
	testutil.DeepEqual(t, tl.VisibleTaskIDs(), legacy)
}

func TestTaskRailEnterCompleteAndHeader(t *testing.T) {
	tl := NewTaskListView()
	tl.SetRailMode(true)
	tl.SetTasks([]*model.Task{{ID: "done", Project: "A", Status: model.StatusComplete}})
	selected := ""
	tl.OnSelect = func(task *model.Task) { selected = task.ID }
	tl.SelectByID("done")
	tl.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	testutil.Equal(t, selected, "done")
	tl.CursorUp()
	tl.InputHandler()(tcell.NewEventKey(tcell.KeyEnter, 0, 0), nil)
	testutil.DeepEqual(t, tl.VisibleTaskIDs(), []string{"done"})
}
