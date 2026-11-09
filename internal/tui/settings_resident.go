package tui

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v2"

	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/tui/theme"
	"github.com/argus-foundry/argus/internal/tui/widget"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// Resident Settings category (add-resident-agent, stage 9). Keys double as the
// DB config keys the daemon's ensure-running loop and db.Config() read; the
// category only ever writes those keys, the daemon picks the change up.
const (
	resKeyEnabled    = "resident.enabled"
	resKeyName       = "resident.name"
	resKeyMaxWorkers = "resident.max_workers"
	resKeyBudget     = "resident.context_budget"
	resKeyMemory     = "resident.memory_dir"
	resKeyModel      = "resident.model"
	resKeyAccount    = "resident.account"
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
		field(resKeyMaxWorkers, "Max workers", strconv.Itoa(r.MaxWorkers), "(default)"),
		field(resKeyMemory, "Memory dir", r.MemoryDir, "(default)"),
		field(resKeyModel, "Model", r.Model, "(default)"),
		field(resKeyAccount, "Account", r.Account, "(default)"),
		{kind: srResident, key: resKeyTools, label: "Tool permissions: config.toml"},
	}
	if sv.residentMaxWorkersOverridden() {
		for i := range rows {
			if rows[i].key == resKeyMaxWorkers {
				rows[i].label += " (config.toml)"
			}
		}
	}
	return rows
}

func (sv *SettingsView) residentMaxWorkersOverridden() bool {
	d := sv.accountDB()
	return d != nil && d.ResidentMaxWorkersFromConfigToml()
}

// SetResidentReason stores the daemon-reported reason the resident is not
// running (empty when healthy). UI goroutine only.
func (sv *SettingsView) SetResidentReason(reason string) {
	sv.residentReason = reason
}

// sanitizeResidentReason keeps a daemon-supplied reason to one printable line
// before it reaches a cell.
func sanitizeResidentReason(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r < 0xa0) {
			return ' '
		}
		return r
	}, s)
	return strings.TrimSpace(s)
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
	if key == resKeyMaxWorkers && sv.residentMaxWorkersOverridden() {
		return sv.residentNotify("max_workers is defined in config.toml — read-only")
	}
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
	case resKeyMaxWorkers:
		buf = strconv.Itoa(r.MaxWorkers)
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
	if key == resKeyMaxWorkers {
		_ = sv.database.Config() // refresh config.toml source metadata before commit
		if sv.residentMaxWorkersOverridden() {
			sv.resEdit = residentEdit{}
			return sv.residentNotify("max_workers is defined in config.toml — read-only")
		}
	}

	switch key {
	case resKeyName:
		if buf == "" && !sv.database.Config().Resident.Enabled {
			break // clearing the name is fine while disabled
		}
		if err := config.ValidResidentName(buf); err != nil {
			return sv.residentNotify("%v", err)
		}
	case resKeyMaxWorkers:
		if _, err := strconv.Atoi(buf); err != nil {
			return sv.residentNotify("max workers must be a number (below 1 uses 8)")
		}
	case resKeyBudget:
		if n, err := strconv.Atoi(buf); err != nil || n <= 0 {
			return sv.residentNotify("context budget must be a positive number")
		}
	case resKeyMemory:
		if buf == "" {
			return sv.residentNotify("memory dir cannot be empty")
		}
		clean := filepath.Clean(buf)
		if filepath.IsAbs(clean) || clean == ".." || strings.HasPrefix(clean, "../") {
			return sv.residentNotify("memory dir must be vault-relative with no '..'")
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
	case resKeyMaxWorkers:
		text = "Maximum live workers on the resident team. Default 8; values below 1 use 8. Ended or graduated workers free a slot."
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
	line := y + 4
	if sv.residentMsg != "" && h > 4 {
		widget.DrawText(screen, x, line, w, sv.residentMsg, theme.StyleDimmed)
		line += 2
	}
	if sv.residentReason != "" && h > line-y {
		widget.DrawText(screen, x, line, w, "Not running: "+sv.residentReason, theme.StyleDimmed)
	}
}
