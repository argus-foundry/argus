package hera

import (
	"strings"
	"testing"

	heramodel "github.com/argus-foundry/argus/internal/hera/model"
	"github.com/argus-foundry/argus/internal/testutil"
	"github.com/gdamore/tcell/v2"
)

func TestGraduationPendingBothRails(t *testing.T) {
	f := newResidentFixture(t)
	b, err := f.d.HeraLiveBindingByTask(f.workers[0])
	testutil.NoError(t, err)
	graduated, err := f.d.GraduateHeraWorker(b.ID)
	testutil.NoError(t, err)
	for _, resident := range []bool{true, false} {
		t.Run(map[bool]string{true: "resident", false: "normal"}[resident], func(t *testing.T) {
			var reader heramodel.HeraReader = f.d
			if resident {
				reader = heramodel.NewResidentReader(f.d, f.d)
			}
			m, err := heramodel.BuildModel(reader, nil, nil, nil, nil)
			testutil.NoError(t, err)
			r := NewRail()
			r.SetNeverCollapse(resident)
			r.SetModel(m)
			sim := tcell.NewSimulationScreen("UTF-8")
			testutil.NoError(t, sim.Init())
			defer sim.Fini()
			sim.SetSize(120, 20)
			r.SetRect(0, 0, 120, 20)
			r.Draw(sim)
			testutil.Contains(t, dumpRows(sim, 120, 20), "graduation pending restart")
			if !resident {
				testutil.Equal(t, m.OrchByID(graduated.NewRole.OrchestratorID).CoordRole().RoleID, graduated.NewRole.ID)
			}
		})
	}
	testutil.NoError(t, f.d.CompleteHeraGraduation(f.workers[0], "orientation"))
	for _, reader := range []heramodel.HeraReader{f.d, heramodel.NewResidentReader(f.d, f.d)} {
		m, err := heramodel.BuildModel(reader, nil, nil, nil, nil)
		testutil.NoError(t, err)
		r := NewRail()
		r.SetModel(m)
		sim := tcell.NewSimulationScreen("UTF-8")
		testutil.NoError(t, sim.Init())
		defer sim.Fini()
		sim.SetSize(120, 20)
		r.SetRect(0, 0, 120, 20)
		r.Draw(sim)
		testutil.False(t, strings.Contains(dumpRows(sim, 120, 20), "graduation pending restart"))
		for _, o := range m.Active {
			for _, r := range o.Roles {
				testutil.True(t, r.RoleID != b.RoleID)
			}
		}
	}
}
