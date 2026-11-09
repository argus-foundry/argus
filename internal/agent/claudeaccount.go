package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/argus-foundry/argus/internal/claudeenv"
	"github.com/argus-foundry/argus/internal/config"
	"github.com/argus-foundry/argus/internal/model"
	"github.com/argus-foundry/argus/internal/uxlog"
)

// claudeSettingsFile is seeded (copied, never linked) into a new account dir.
const claudeSettingsFile = "settings.json"

// claudeAuthOverrideEnv are auth sources Claude Code ranks above the stored
// /login, so an inherited value would silently bill a different identity than
// the task's explicit account.
var claudeAuthOverrideEnv = claudeenv.AuthOverrides

// claudeSettingsAccountKeys are settings.json keys bound to the default login or
// able to run credential-fetching commands, so they are never copied into
// another account's dir. env is dropped whole because it can carry any secret.
var claudeSettingsAccountKeys = []string{
	"apiKeyHelper", "env", "forceLoginMethod", "forceLoginOrgUUID",
	"awsAuthRefresh", "awsCredentialExport", "otelHeadersHelper",
}

// BootstrapClaudeConfigDir creates dir (0700), symlinks each inherit entry
// that exists in Claude's default config dir into it, and seeds a one-time
// copy of the default settings.json. A nil inherit uses
// config.DefaultClaudeInherit. Entries already present in dir are never
// touched, and credential/global-state entries are never linked, so the
// operation is idempotent and never reads or writes credentials.
func BootstrapClaudeConfigDir(dir string, inherit []string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("creating claude account dir %s: %w", dir, err)
	}
	if inherit == nil {
		inherit = config.DefaultClaudeInherit()
	}
	src := config.DefaultClaudeDir()
	if src == "" {
		return nil
	}
	for _, name := range inherit {
		if name == "" || name != filepath.Base(name) || config.NeverInheritClaude(name) {
			uxlog.Log("[account] skip inherit entry %q", name)
			continue
		}
		from := filepath.Join(src, name)
		if _, err := os.Stat(from); err != nil {
			continue
		}
		to := filepath.Join(dir, name)
		if _, err := os.Lstat(to); err == nil {
			continue
		}
		if err := os.Symlink(from, to); err != nil {
			return fmt.Errorf("linking %s into claude account dir: %w", name, err)
		}
	}
	return seedClaudeSettings(src, dir)
}

// seedClaudeSettings copies <src>/settings.json into dir when dir has no
// settings.json entry of any kind. A copy (not a symlink) keeps Claude's own
// settings writes inside the account; O_EXCL refuses an existing path,
// including a symlink, so nothing is ever overwritten or written through.
// Login-bound and credential-fetching keys (claudeSettingsAccountKeys) are
// dropped so the copy cannot outrank or break the account's own /login.
func seedClaudeSettings(src, dir string) error {
	to := filepath.Join(dir, claudeSettingsFile)
	if _, err := os.Lstat(to); err == nil {
		return nil
	}
	raw, err := os.ReadFile(filepath.Join(src, claudeSettingsFile))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			uxlog.Log("[account] settings seed skipped: %v", err)
		}
		return nil
	}
	out, dropped, err := stripClaudeSettingsAuth(raw)
	if err != nil {
		uxlog.Log("[account] settings seed skipped: unparsable %s: %v", claudeSettingsFile, err)
		return nil
	}
	if len(dropped) > 0 {
		uxlog.Log("[account] settings seed dropped auth overrides: %s", strings.Join(dropped, ","))
	}
	f, err := os.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return nil
		}
		return fmt.Errorf("seeding claude account settings: %w", err)
	}
	if _, werr := f.Write(out); werr != nil {
		_ = f.Close()
		return fmt.Errorf("seeding claude account settings: %w", werr)
	}
	if cerr := f.Close(); cerr != nil {
		return fmt.Errorf("seeding claude account settings: %w", cerr)
	}
	uxlog.Log("[account] seeded %s into %s", claudeSettingsFile, dir)
	return nil
}

// stripClaudeSettingsAuth removes auth-overriding keys from a settings.json
// document, returning the original bytes untouched when nothing is dropped.
// dropped holds key names only.
func stripClaudeSettingsAuth(raw []byte) (out []byte, dropped []string, err error) {
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, nil, err
	}
	for _, k := range claudeSettingsAccountKeys {
		if _, ok := doc[k]; ok {
			delete(doc, k)
			dropped = append(dropped, k)
		}
	}
	if len(dropped) == 0 {
		return raw, nil, nil
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	return append(b, '\n'), dropped, nil
}

// resolveSpawnClaudeConfigDir returns the explicit Claude config dir a Claude
// task spawns under, bootstrapping it, or "" for the default account and for
// non-Claude backends. An unknown/removed stored account is an error: a spawn
// must never silently fall back to the default identity.
func resolveSpawnClaudeConfigDir(task *model.Task, cfg config.Config, isClaude bool) (string, error) {
	if !isClaude || task.Account == "" || task.Account == config.DefaultAccountName {
		return "", nil
	}
	dir, explicit, err := cfg.ClaudeConfigDir(task.Account)
	if err != nil {
		uxlog.Log("[account] task %q: %v", task.ID, err)
		return "", fmt.Errorf("claude account %q: %w", task.Account, err)
	}
	if !explicit {
		return "", nil
	}
	if err := BootstrapClaudeConfigDir(dir, cfg.Accounts[task.Account].Inherit); err != nil {
		uxlog.Log("[account] task %q bootstrap failed: %v", task.ID, err)
		return "", fmt.Errorf("claude account %q: %w", task.Account, err)
	}
	return dir, nil
}

// spawnAuthStrip lists the inherited auth-override variables to drop for a
// task on an explicit Claude and/or Codex account.
func spawnAuthStrip(claudeExplicit, codexExplicit bool) []string {
	var names []string
	if claudeExplicit {
		names = append(names, claudeAuthOverrideEnv...)
	}
	if codexExplicit {
		names = append(names, codexAuthOverrideEnv...)
	}
	return names
}

// spawnBaseEnv filters the inherited environment for a spawned session. An
// inherited CLAUDE_CONFIG_DIR is always dropped (the task's own account
// decides it), as is every parent Claude Code session marker
// (claudeenv.ParentSessionMarkers — present whenever the daemon/supervisor was
// started from inside Claude Code, for every backend), and every name in
// authStrip (the auth overrides a backend ranks above an explicit account's
// login). Backend env_vars and the op bootstrap are appended after this, so
// explicit mappings still win. Only stripped NAMES are logged, never values.
func spawnBaseEnv(environ []string, authStrip []string, taskID string) []string {
	strip := map[string]bool{"CLAUDE_CONFIG_DIR": true}
	for _, k := range claudeenv.ParentSessionMarkers {
		strip[k] = true
	}
	for _, k := range authStrip {
		strip[k] = true
	}
	out := make([]string, 0, len(environ))
	removed := map[string]bool{}
	for _, kv := range environ {
		name, _, _ := strings.Cut(kv, "=")
		if strip[name] {
			removed[name] = true
			continue
		}
		out = append(out, kv)
	}
	if len(removed) > 0 {
		names := make([]string, 0, len(removed))
		for n := range removed {
			names = append(names, n)
		}
		sort.Strings(names)
		uxlog.Log("[account] task %q: stripped inherited env %s", taskID, strings.Join(names, ","))
	}
	return out
}

// sandboxWithClaudeConfigDir appends the task's own account Claude dir to
// ExtraWrite when it lies outside the default Claude dir (already writable),
// so /login token persistence inside the sandbox does not EPERM.
func sandboxWithClaudeConfigDir(sc config.SandboxConfig, dir string) config.SandboxConfig {
	if dir == "" {
		return sc
	}
	def := config.DefaultClaudeDir()
	if dir == def || (def != "" && strings.HasPrefix(dir, def+string(filepath.Separator))) {
		return sc
	}
	sc.ExtraWrite = append(append([]string(nil), sc.ExtraWrite...), dir)
	return sc
}

// claudeConfigDirFromEnv returns the CLAUDE_CONFIG_DIR a session was spawned
// with (last assignment wins, as in exec.Cmd), or "" for the default account.
func claudeConfigDirFromEnv(env []string) string {
	dir := ""
	for _, kv := range env {
		if v, ok := strings.CutPrefix(kv, "CLAUDE_CONFIG_DIR="); ok {
			dir = v
		}
	}
	return dir
}

// residentExecutable resolves the argus binary the resident's inline Stop hook
// runs. A package var so tests can pin it.
var residentExecutable = os.Executable

// residentStopHookCommand is the inline `coord-hook` Stop hook that replaces the
// user-level one the resident's restricted setting sources drop. The global
// registration is the bare `argus coord-hook` (resolved through PATH); the
// resident pins the absolute path of the binary doing the spawn (daemon or
// supervisor, both the argus executable) so the hook never depends on PATH
// or a hard-coded install location. Falls back to the bare name.
func residentStopHookCommand() string {
	exe, err := residentExecutable()
	if err != nil || exe == "" {
		return "argus coord-hook"
	}
	if !shellSafeWord.MatchString(exe) {
		exe = shellQuote(exe)
	}
	return exe + " coord-hook"
}

// shellSafeWord matches a path needing no shell quoting.
var shellSafeWord = regexp.MustCompile(`^[A-Za-z0-9_./+@%:,=-]+$`)

// residentUserSettings is the slice of an account's user settings.json the
// resident re-supplies: restrictions (deny/ask) and the model it should keep.
// Allow rules are never read.
type residentUserSettings struct {
	Ask, Deny     []string
	Model, Effort string
	// Sandbox is the user's Claude `sandbox` block with its permission-widening
	// keys neutralized (see residentSandbox), carried inline.
	Sandbox json.RawMessage
	// SkippedHooks names the user hook events the restricted setting sources
	// drop (hooks are never carried: one may auto-approve; see gotchas).
	SkippedHooks []string
}

// residentAccountDir is the Claude config directory the resident's account
// loads user-level files from: the task's account dir, else Claude's default
// ~/.claude. An inherited $CLAUDE_CONFIG_DIR is deliberately ignored: spawnBaseEnv
// strips it, so the default-account resident really runs on ~/.claude. Empty
// when unresolvable.
func residentAccountDir(task *model.Task, cfg config.Config) string {
	dir, err := ClaudeConfigDirForTask(task, cfg)
	if err != nil {
		uxlog.Log("[resident] account config dir unresolved (skipping user-level files): %v", err)
		slog.Warn("[resident] account config dir unresolved", "err", err)
		return ""
	}
	if dir == "" {
		// A backend mapping that resolves to unset launches on Claude's default.
		dir = config.DefaultClaudeDir()
	}
	return dir
}

// readResidentUserSettings best-effort reads the account's settings.json. A
// missing or unreadable file yields the zero value (the resident's own lists).
func readResidentUserSettings(task *model.Task, cfg config.Config) residentUserSettings {
	dir := residentAccountDir(task, cfg)
	if dir == "" {
		return residentUserSettings{}
	}
	path := filepath.Join(dir, claudeSettingsFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			uxlog.Log("[resident] user settings unreadable (using the resident's own rules only): %v", err)
			slog.Warn("[resident] user settings unreadable", "path", path, "err", err)
		}
		return residentUserSettings{}
	}
	var doc struct {
		Permissions struct {
			Ask  []any `json:"ask"`
			Deny []any `json:"deny"`
		} `json:"permissions"`
		Model       any                        `json:"model"`
		EffortLevel any                        `json:"effortLevel"`
		Sandbox     json.RawMessage            `json:"sandbox"`
		Hooks       map[string]json.RawMessage `json:"hooks"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		uxlog.Log("[resident] user settings unparseable (using the resident's own rules only): %v", err)
		slog.Warn("[resident] user settings unparseable", "path", path, "err", err)
		return residentUserSettings{}
	}
	strs := func(in []any) []string {
		var out []string
		for _, v := range in {
			if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
				out = append(out, s)
			}
		}
		return out
	}
	str := func(v any) string {
		s, _ := v.(string)
		return strings.TrimSpace(s)
	}
	us := residentUserSettings{
		Ask: strs(doc.Permissions.Ask), Deny: strs(doc.Permissions.Deny),
		Model: str(doc.Model), Effort: str(doc.EffortLevel),
	}
	us.Sandbox = residentSandbox(doc.Sandbox)
	for event := range doc.Hooks {
		us.SkippedHooks = append(us.SkippedHooks, event)
	}
	sort.Strings(us.SkippedHooks)
	if len(us.SkippedHooks) > 0 {
		uxlog.Log("[resident] user hooks not applied to the resident (settings isolated): %s", strings.Join(us.SkippedHooks, ", "))
		slog.Info("[resident] user hooks not applied to the resident (settings isolated)", "events", strings.Join(us.SkippedHooks, ","))
	}
	return us
}

// mergeRules returns own followed by extra, without duplicates.
func mergeRules(own, extra []string) []string {
	out := append([]string(nil), own...)
	seen := make(map[string]bool, len(own)+len(extra))
	for _, r := range own {
		seen[r] = true
	}
	for _, r := range extra {
		if !seen[r] {
			seen[r] = true
			out = append(out, r)
		}
	}
	return out
}

// readResidentUserClaudeMD returns the account's CLAUDE.md, or "" when absent.
func readResidentUserClaudeMD(task *model.Task, cfg config.Config) string {
	dir := residentAccountDir(task, cfg)
	if dir == "" {
		return ""
	}
	raw, err := os.ReadFile(filepath.Join(dir, "CLAUDE.md"))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			uxlog.Log("[resident] user CLAUDE.md unreadable (skipping): %v", err)
			slog.Warn("[resident] user CLAUDE.md unreadable", "err", err)
		}
		return ""
	}
	return strings.TrimSpace(string(raw))
}

// materializeResidentPrompt writes routingPath's content (when set) followed by
// the user CLAUDE.md as one content-addressed file and returns its path.
func materializeResidentPrompt(routingPath, claudeMD string) (string, error) {
	var buf []byte
	if routingPath != "" {
		b, err := os.ReadFile(routingPath)
		if err != nil {
			return "", fmt.Errorf("read routing prompt: %w", err)
		}
		buf = append(buf, b...)
		buf = append(buf, '\n')
	}
	buf = append(buf, "# User instructions (CLAUDE.md)\n\n"...)
	buf = append(buf, claudeMD...)
	buf = append(buf, '\n')
	root, err := argusCacheDir("resident-prompt")
	if err != nil {
		return "", err
	}
	dir, err := materializeContentAddressed(root, map[string][]byte{"system-prompt.md": buf})
	if err != nil {
		return "", err
	}
	file := filepath.Join(dir, "system-prompt.md")
	// ~/.argus/cache is writable by every sandboxed agent and a populated hash dir is
	// reused on its marker alone, so a tampered file would become the resident's
	// system prompt. Verify the bytes and atomically restore them when they differ.
	if got, rerr := os.ReadFile(file); rerr != nil || !bytes.Equal(got, buf) {
		uxlog.Log("[resident] system prompt file differed from expected; restoring %s", file)
		slog.Warn("[resident] system prompt file tampered or missing; restoring", "path", file)
		if err := atomicWriteFile(file, buf); err != nil {
			return "", fmt.Errorf("restore resident prompt: %w", err)
		}
	}
	return file, nil
}

// atomicWriteFile writes data to a temp file beside path and renames it over
// path. A rename replaces a tampered symlink or file rather than writing
// through it.
func atomicWriteFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".restore-")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		_ = os.Remove(name)
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// ensureResidentUserSkillsDir returns an --add-dir root whose .claude/skills
// links to the account's user skills dir, or "" when the account has none.
// Claude discovers skills only under <added dir>/.claude/skills; the link lives
// in the Argus cache, keyed by the source path so accounts never share one.
func ensureResidentUserSkillsDir(task *model.Task, cfg config.Config) string {
	dir := residentAccountDir(task, cfg)
	if dir == "" {
		return ""
	}
	src := filepath.Join(dir, "skills")
	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		return ""
	}
	cache, err := argusCacheDir("resident-skills")
	if err != nil {
		return ""
	}
	sum := sha256.Sum256([]byte(src))
	root := filepath.Join(cache, hex.EncodeToString(sum[:])[:16])
	link := filepath.Join(root, ".claude", "skills")
	if cur, err := os.Readlink(link); err == nil && cur == src {
		return root
	}
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		uxlog.Log("[resident] user skills link dir failed (skipping user skills): %v", err)
		slog.Warn("[resident] user skills link dir failed", "err", err)
		return ""
	}
	_ = os.Remove(link)
	if err := os.Symlink(src, link); err != nil {
		uxlog.Log("[resident] user skills link failed (skipping user skills): %v", err)
		slog.Warn("[resident] user skills link failed", "err", err)
		return ""
	}
	return root
}

// residentSandbox carries the user's Claude `sandbox` block for its
// restrictions only. The block is NOT restrict-only as written: with sandboxing
// enabled, autoAllowBashIfSandboxed (default true) auto-approves Bash, and
// excludedCommands / allowUnsandboxedCommands loosen it. So the resident gets
// the block with autoAllowBashIfSandboxed and allowUnsandboxedCommands forced
// false and excludedCommands removed; every Bash call still prompts. A
// missing or non-object block yields nil.
func residentSandbox(raw json.RawMessage) json.RawMessage {
	var block map[string]any
	if s := bytes.TrimSpace(raw); len(s) == 0 || s[0] != '{' || json.Unmarshal(s, &block) != nil {
		return nil
	}
	block["autoAllowBashIfSandboxed"] = false
	block["allowUnsandboxedCommands"] = false
	delete(block, "excludedCommands")
	out, err := json.Marshal(block)
	if err != nil {
		return nil
	}
	return out
}
