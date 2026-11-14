package keymap

import "testing"

// Every CtxHeraRail action must be explicitly classified for the resident tab:
// a new rail action defaults to inert only because someone wrote that down.
func TestResidentRailPolicyClassifiesEveryRailAction(t *testing.T) {
	for _, act := range ContextOrder(CtxHeraRail) {
		if _, ok := residentRailPolicy[act]; !ok {
			t.Errorf("rail action %q has no resident policy entry", act)
		}
	}
	for act := range residentRailPolicy {
		found := false
		for _, a := range ContextOrder(CtxHeraRail) {
			if a == act {
				found = true
			}
		}
		if !found {
			t.Errorf("resident policy lists %q, which is not a rail action", act)
		}
	}
}

func TestResidentRailAccess(t *testing.T) {
	cases := []struct {
		act  Action
		want ResidentAccess
	}{
		{ActHeraDelete, ResidentWorkerOnly},
		{ActHeraArchive, ResidentWorkerOnly},
		{ActHeraRename, ResidentWorkerOnly},
		{ActHeraStatAdv, ResidentWorkerOnly},
		{ActHeraStatRev, ResidentWorkerOnly},
		{ActHeraInbox, ResidentAnywhere},
		{ActHeraSpawn, ResidentInert},
		{ActHeraNewCoord, ResidentInert},
		{ActHeraPin, ResidentInert},
		{ActHeraKanbanAdv, ResidentInert},
		{ActHeraKanbanRev, ResidentInert},
		{ActHeraAdopt, ResidentInert},
		{ActHeraClear, ResidentInert},
		{ActHeraCleanup, ResidentInert},
		{ActHeraMoveAccount, ResidentOwnRowOnly},
		{Action("hera_rail.unknown"), ResidentInert},
	}
	for _, c := range cases {
		t.Run(string(c.act), func(t *testing.T) {
			if got := ResidentRailAccess(c.act); got != c.want {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestResidentRailActionsFollowContextOrder(t *testing.T) {
	got := ResidentRailActions()
	want := []Action{ActHeraRename, ActHeraArchive, ActHeraStatAdv, ActHeraStatRev, ActHeraInbox, ActHeraMoveAccount, ActHeraDelete}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestResidentHelpRowsUseLiveBindings(t *testing.T) {
	rows := DefaultKeymap().ResidentHelpRows()
	if len(rows) != 7 {
		t.Fatalf("rows=%v", rows)
	}
	for _, r := range rows {
		if r.Key == "" || r.Label == "" {
			t.Fatalf("empty row %+v", r)
		}
	}
	for _, r := range rows {
		if r.Label == actionLabels[ActHeraSpawn] || r.Label == actionLabels[ActHeraPin] {
			t.Fatalf("creation/structure key leaked into resident help: %+v", r)
		}
	}
}
