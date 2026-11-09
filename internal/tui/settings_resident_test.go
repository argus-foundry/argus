package tui

import (
	"testing"

	"github.com/gdamore/tcell/v2"

	"github.com/argus-foundry/argus/internal/testutil"
)

func residentTypeText(sv *SettingsView, s string) {
	for _, r := range s {
		sv.HandleKey(tcell.NewEventKey(tcell.KeyRune, r, tcell.ModNone))
	}
}

func residentEditField(t *testing.T, sv *SettingsView, key, text string) {
	t.Helper()
	selectRowInCategory(t, sv, catResident, srResident, key)
	testutil.True(t, sv.handleEnter())
	testutil.True(t, sv.IsEditing())
	// Clear the prefilled buffer.
	for sv.resEdit.buf != "" {
		sv.HandleKey(tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone))
	}
	residentTypeText(sv, text)
	sv.HandleKey(tcell.NewEventKey(tcell.KeyEnter, 0, tcell.ModNone))
}

func residentEnableKB(t *testing.T, sv *SettingsView) {
	t.Helper()
	testutil.NoError(t, sv.database.SetConfigValue("kb.enabled", "true"))
	testutil.NoError(t, sv.database.SetConfigValue("api.enabled", "true"))
	testutil.NoError(t, sv.database.SetConfigValue("kb.metis_vault_path", t.TempDir()))
}

func TestSettingsResident_CategoryAndRows(t *testing.T) {
	sv := testSettingsView(t)
	testutil.Equal(t, catResident.Label(), "Resident")
	sv.setCategory(catResident)
	testutil.Equal(t, len(sv.rows), 8)
	selectRowInCategory(t, sv, catResident, srResident, resKeyName)
	testutil.Contains(t, sv.SelectedRow().label, "(required)")
	selectRowInCategory(t, sv, catResident, srResident, resKeyTools)
	testutil.Contains(t, sv.SelectedRow().label, "config.toml")
	testutil.True(t, sv.handleEnter()) // read-only pointer: no-op
	testutil.False(t, sv.IsEditing())
}

func TestSettingsResidentMaxWorkers(t *testing.T) {
	sv := testSettingsView(t)
	residentEditField(t, sv, resKeyMaxWorkers, "3")
	testutil.Equal(t, sv.database.Config().Resident.MaxWorkers, 3)
	residentEditField(t, sv, resKeyMaxWorkers, "bad")
	testutil.True(t, sv.IsEditing())
	testutil.Equal(t, sv.database.Config().Resident.MaxWorkers, 3)
}

func TestSettingsResidentMaxWorkersOverride(t *testing.T) {
	sv := testSettingsViewWithConfigToml(t, "[resident]\nmax_workers = 3\n")
	sv.setCategory(catResident)
	selectRowInCategory(t, sv, catResident, srResident, resKeyMaxWorkers)
	testutil.Contains(t, sv.SelectedRow().label, "3 (config.toml)")
	sv.handleEnter()
	testutil.False(t, sv.IsEditing())
	testutil.Contains(t, sv.residentMsg, "read-only")
}

func TestSettingsResident_EnableBlockedUntilNamed(t *testing.T) {
	sv := testSettingsView(t)
	residentEnableKB(t, sv)
	selectRowInCategory(t, sv, catResident, srResident, resKeyEnabled)
	testutil.True(t, sv.handleEnter())
	testutil.False(t, sv.database.Config().Resident.Enabled)
	testutil.Contains(t, sv.residentMsg, "name")
}

func TestSettingsResident_EnableBlockedByPrereq(t *testing.T) {
	sv := testSettingsView(t)
	testutil.NoError(t, sv.database.SetConfigValue("api.enabled", "true"))
	testutil.NoError(t, sv.database.SetConfigValue("kb.enabled", "false"))
	residentEditField(t, sv, resKeyName, "Solo")
	selectRowInCategory(t, sv, catResident, srResident, resKeyEnabled)
	testutil.True(t, sv.handleEnter())
	testutil.False(t, sv.database.Config().Resident.Enabled)
	testutil.Contains(t, sv.residentMsg, "kb.enabled")
}

func TestSettingsResident_EnableBlockedWithoutAPI(t *testing.T) {
	sv := testSettingsView(t)
	residentEnableKB(t, sv)
	testutil.NoError(t, sv.database.SetConfigValue("api.enabled", "false"))
	residentEditField(t, sv, resKeyName, "Solo")
	selectRowInCategory(t, sv, catResident, srResident, resKeyEnabled)
	testutil.True(t, sv.handleEnter())
	testutil.False(t, sv.database.Config().Resident.Enabled)
	testutil.Contains(t, sv.residentMsg, "api.enabled")
}

func TestSettingsResident_EnableAndLiveRename(t *testing.T) {
	sv := testSettingsView(t)
	residentEnableKB(t, sv)
	var gotName string
	var gotEnabled bool
	calls := 0
	sv.OnResidentChange = func(n string, e bool) { gotName, gotEnabled = n, e; calls++ }

	residentEditField(t, sv, resKeyName, "Solo")
	testutil.Equal(t, sv.database.Config().Resident.Name, "Solo")
	selectRowInCategory(t, sv, catResident, srResident, resKeyEnabled)
	testutil.True(t, sv.handleEnter())
	testutil.True(t, sv.database.Config().Resident.Enabled)
	testutil.Equal(t, gotName, "Solo")
	testutil.True(t, gotEnabled)

	// Live rename while enabled: visible through db.Config() and the callback.
	residentEditField(t, sv, resKeyName, "Ari")
	testutil.Equal(t, sv.database.Config().Resident.Name, "Ari")
	testutil.Equal(t, gotName, "Ari")
	testutil.True(t, gotEnabled)
	testutil.Equal(t, calls, 3)
	selectRowInCategory(t, sv, catResident, srResident, resKeyName)
	testutil.Contains(t, sv.SelectedRow().label, "Ari")

	// Disabling works too.
	selectRowInCategory(t, sv, catResident, srResident, resKeyEnabled)
	testutil.True(t, sv.handleEnter())
	testutil.False(t, sv.database.Config().Resident.Enabled)
}

func TestSettingsResident_Validation(t *testing.T) {
	sv := testSettingsView(t)
	residentEditField(t, sv, resKeyName, "ThisNameIsFarTooLong")
	testutil.True(t, sv.IsEditing()) // rejected, stays open
	testutil.Contains(t, sv.residentMsg, "1 to 12")
	testutil.Equal(t, sv.database.Config().Resident.Name, "")
	sv.HandleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	testutil.False(t, sv.IsEditing())

	residentEditField(t, sv, resKeyBudget, "abc")
	testutil.True(t, sv.IsEditing())
	sv.HandleKey(tcell.NewEventKey(tcell.KeyEscape, 0, tcell.ModNone))
	residentEditField(t, sv, resKeyBudget, "123456")
	testutil.Equal(t, sv.database.Config().Resident.ContextBudget, 123456)

	residentEditField(t, sv, resKeyMemory, "notes")
	testutil.Equal(t, sv.database.Config().Resident.MemoryDir, "notes")
	residentEditField(t, sv, resKeyModel, "opus")
	testutil.Equal(t, sv.database.Config().Resident.Model, "opus")
	residentEditField(t, sv, resKeyAccount, "work")
	testutil.Equal(t, sv.database.Config().Resident.Account, "work")

	// Enabled resident cannot lose its name.
	residentEnableKB(t, sv)
	residentEditField(t, sv, resKeyName, "Solo")
	selectRowInCategory(t, sv, catResident, srResident, resKeyEnabled)
	sv.handleEnter()
	residentEditField(t, sv, resKeyName, "")
	testutil.True(t, sv.IsEditing())
	testutil.Equal(t, sv.database.Config().Resident.Name, "Solo")
}

func TestSettingsResident_RemoteReadOnly(t *testing.T) {
	sv := testSettingsView(t)
	sv.remote = true
	selectRowInCategory(t, sv, catResident, srResident, resKeyName)
	testutil.True(t, sv.handleEnter())
	testutil.False(t, sv.IsEditing())
	testutil.Contains(t, sv.residentMsg, "read-only")
}

func TestSettingsResident_MemoryDirMustBeVaultRelative(t *testing.T) {
	for _, bad := range []string{"../x", "/abs", "..", "a/../../b"} {
		t.Run(bad, func(t *testing.T) {
			sv := testSettingsView(t)
			before := sv.database.Config().Resident.MemoryDir
			residentEditField(t, sv, resKeyMemory, bad)
			testutil.True(t, sv.IsEditing()) // rejected edit stays open
			testutil.Contains(t, sv.residentMsg, "vault-relative")
			testutil.Equal(t, sv.database.Config().Resident.MemoryDir, before)
		})
	}
	sv := testSettingsView(t)
	residentEditField(t, sv, resKeyMemory, "assistant/mem")
	testutil.False(t, sv.IsEditing())
	testutil.Equal(t, sv.database.Config().Resident.MemoryDir, "assistant/mem")
}

func TestSettingsResident_PasteNormalizesWhitespace(t *testing.T) {
	sv := testSettingsView(t)
	selectRowInCategory(t, sv, catResident, srResident, resKeyName)
	sv.handleEnter()
	for sv.resEdit.buf != "" {
		sv.HandleKey(tcell.NewEventKey(tcell.KeyBackspace2, 0, tcell.ModNone))
	}
	sv.PasteHandler()("A\n r i", nil)
	testutil.Equal(t, sv.resEdit.buf, "A r i")
	sv.SetRect(0, 0, 100, 30)
	sim := drawSim(t)
	sv.Draw(sim)
	sim.Show()
	testutil.Contains(t, screenText(sim, 100, 30), "A r i▎")
}

func TestSettingsResident_PasteAndRender(t *testing.T) {
	sv := testSettingsView(t)
	selectRowInCategory(t, sv, catResident, srResident, resKeyName)
	sv.handleEnter()
	sv.resEdit.buf += "Ari"
	sv.rebuildRows()
	sv.SetRect(0, 0, 100, 30)
	sv.Draw(drawSim(t))
	for _, k := range []string{resKeyEnabled, resKeyBudget, resKeyTools} {
		sv.resEdit = residentEdit{}
		selectRowInCategory(t, sv, catResident, srResident, k)
		sv.Draw(drawSim(t))
	}
}
