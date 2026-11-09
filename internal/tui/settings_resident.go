package tui

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"

	"github.com/drn/argus/internal/config"
	"github.com/drn/argus/internal/tui/theme"
	"github.com/drn/argus/internal/tui/widget"
	"github.com/drn/argus/internal/uxlog"
)

// Resident Settings category (add-resident-agent, stage 9). Keys double as the
// DB config keys the daemon's ensure-running loop and db.Config() read; the
// category only ever writes those keys, the daemon picks the change up.
const (
	resKeyEnabled = "resident.enabled"
	resKeyName    = "resident.name"
	resKeyBudget  = "resident.context_budget"
	resKeyMemory  = "resident.memory_dir"
	resKeyModel   = "resident.model"
	resKeyAccount = "resident.account"
	// resKeyTools is the read-only pointer row (allow_tools/ask_tools/deny_tools live in
	// config.toml only).
	resKeyTools = "resident.tools"
)

// residentEdit is the single inline text editor shared by the editable rows.
type residentEdit struct {
	active bool
	key    string
	buf    string
}

// residentEditable reports whether resident settings can be written from this
// view: local *db.DB only, like Accounts.
func (sv *SettingsView) residentEditable() bool {
	return !sv.remote && sv.accountDB() != nil
}

func (sv *SettingsView) residentRows() []settingsRow {
	r := sv.database.Config().Resident
	field := func(key, label, val, empty string) settingsRow {
		switch {
		case sv.resEdit.active && sv.resEdit.key == key:
			val = sv.resEdit.buf + "▎"
		case val == "":
			val = empty
		}
		return settingsRow{kind: srResident, key: key, label: label + ": " + val}
	}
	state := "OFF"
	if r.Enabled {
		state = "ON"
	}
	rows := []settingsRow{
		{kind: srResident, key: resKeyEnabled, label: "Enabled: " + state},
		field(resKeyName, "Name", r.Name, "(required)"),
		field(resKeyBudget, "Context budget", strconv.Itoa(r.ContextBudget), "(default)"),
		field(resKeyMemory, "Memory dir", r.MemoryDir, "(default)"),
		field(resKeyModel, "Model", r.Model, "(default)"),
		field(resKeyAccount, "Account", r.Account, "(default)"),
		{kind: srResident, key: resKeyTools, label: "Tool permissions: config.toml"},
	}
	return rows
}

// residentNotify records a user-facing message shown in the detail pane.
func (sv *SettingsView) residentNotify(format string, args ...any) bool {
	sv.residentMsg = fmt.Sprintf(format, args...)
	uxlog.Log("[resident] settings: %s", sv.residentMsg)
	sv.rebuildRows()
	return true
}

// persistResident writes one key and fires OnResidentChange.
func (sv *SettingsView) persistResident(key, val string) bool {
	if err := sv.database.SetConfigValue(key, val); err != nil {
		return sv.residentNotify("could not save %s: %v", key, err)
	}
	sv.residentMsg = ""
	uxlog.Log("[resident] settings saved key=%s value=%q", key, val)
	sv.rebuildRows()
	if sv.OnResidentChange != nil {
		r := sv.database.Config().Resident
		sv.OnResidentChange(r.Name, r.Enabled)
	}
	return true
}

// handleResidentEnter toggles enabled or starts inline editing of a field.
func (sv *SettingsView) handleResidentEnter(key string) bool {
	if key == resKeyTools {
		return true
	}
	if !sv.residentEditable() {
		return sv.residentNotify("Resident settings are read-only in --remote mode")
	}
	if key == resKeyEnabled {
		return sv.toggleResidentEnabled()
	}
	r := sv.database.Config().Resident
	buf := ""
	switch key {
	case resKeyName:
		buf = r.Name
	case resKeyBudget:
		buf = strconv.Itoa(r.ContextBudget)
	case resKeyMemory:
		buf = r.MemoryDir
	case resKeyModel:
		buf = r.Model
	case resKeyAccount:
		buf = r.Account
	}
	sv.residentMsg = ""
	sv.resEdit = residentEdit{active: true, key: key, buf: buf}
	sv.rebuildRows()
	return true
}

// toggleResidentEnabled gates enabling on a valid name and the KB prerequisite.
// A refused enable persists nothing.
func (sv *SettingsView) toggleResidentEnabled() bool {
	cfg := sv.database.Config()
	if cfg.Resident.Enabled {
		return sv.persistResident(resKeyEnabled, "false")
	}
	if err := config.ValidResidentName(cfg.Resident.Name); err != nil {
		return sv.residentNotify("Choose a name for your resident first (1 to %d characters), then enable it", config.ResidentNameMaxRunes)
	}
	cfg.Resident.Enabled = true
	if err := cfg.ResidentPrereq(); err != nil {
		return sv.residentNotify("Cannot enable: %v", err)
	}
	return sv.persistResident(resKeyEnabled, "true")
}

// commitResidentEdit validates and persists the finished edit. A rejected
// edit stays open with the reason shown.
func (sv *SettingsView) commitResidentEdit() bool {
	key, buf := sv.resEdit.key, strings.TrimSpace(sv.resEdit.buf)
	switch key {
	case resKeyName:
		if buf == "" && !sv.database.Config().Resident.Enabled {
			break // clearing the name is fine while disabled
		}
		if err := config.ValidResidentName(buf); err != nil {
			return sv.residentNotify("%v", err)
		}
	case resKeyBudget:
		if n, err := strconv.Atoi(buf); err != nil || n <= 0 {
			return sv.residentNotify("context budget must be a positive number")
		}
	case resKeyMemory:
		if buf == "" {
			return sv.residentNotify("memory dir cannot be empty")
		}
	}
	sv.resEdit = residentEdit{}
	return sv.persistResident(key, buf)
}

func (sv *SettingsView) handleResidentEditKey(ev *tcell.EventKey) bool {
	switch ev.Key() {
	case tcell.KeyEnter:
		return sv.commitResidentEdit()
	case tcell.KeyEscape:
		sv.resEdit = residentEdit{}
		sv.residentMsg = ""
		sv.rebuildRows()
		return true
	case tcell.KeyDown, tcell.KeyUp, tcell.KeyLeft, tcell.KeyRight:
		return true
	case tcell.KeyBackspace, tcell.KeyBackspace2:
		if sv.resEdit.buf != "" {
			_, size := utf8.DecodeLastRuneInString(sv.resEdit.buf)
			sv.resEdit.buf = sv.resEdit.buf[:len(sv.resEdit.buf)-size]
			sv.rebuildRows()
		}
		return true
	case tcell.KeyRune:
		sv.resEdit.buf += string(ev.Rune())
		sv.rebuildRows()
		return true
	}
	return false
}

func (sv *SettingsView) renderResidentDetail(screen tcell.Screen, x, y, w, h int, row *settingsRow) {
	widget.DrawText(screen, x, y, w, "Resident agent", theme.StyleTitle)
	text := "Always-on assistant with its own tab and hotkey. Enabling requires a name and the knowledge base."
	switch row.key {
	case resKeyName:
		text = fmt.Sprintf("Display name (1 to %d characters), used for the tab label. Renaming applies live.", config.ResidentNameMaxRunes)
	case resKeyBudget:
		text = "Context size at which the resident is nudged to recycle."
	case resKeyMemory:
		text = "Vault-relative folder for working memory. Independent of the name."
	case resKeyModel:
		text = "Optional model override. Empty uses the resolved default."
	case resKeyAccount:
		text = "Optional account override. Empty uses the default account."
	case resKeyTools:
		text = "allow_tools, ask_tools and deny_tools are edited in config.toml under [resident]."
	}
	widget.DrawText(screen, x, y+2, w, text, theme.StyleDimmed)
	if sv.residentMsg != "" && h > 4 {
		widget.DrawText(screen, x, y+4, w, sv.residentMsg, theme.StyleDimmed)
	}
}
