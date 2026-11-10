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
	"math/rand/v2"
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
// login). An inherited ARGUS_ARTIFACT_ACCOUNT is always dropped too: BuildCmd
// recomputes it per task, and a parent's value must never reach a task that
// should not see it (logged under [artifacts], not [account]). Backend
// env_vars and the op bootstrap are appended after this, so explicit mappings
// still win. Only stripped NAMES are logged, never values.
func spawnBaseEnv(environ []string, authStrip []string, taskID string) []string {
	strip := map[string]bool{"CLAUDE_CONFIG_DIR": true, artifactAccountEnv: true}
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
	if removed[artifactAccountEnv] {
		delete(removed, artifactAccountEnv)
		uxlog.Log("[artifacts] task %q: stripped inherited %s (recomputed per task)", taskID, artifactAccountEnv)
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

// artifactAccountEnv names the artifact home account BuildCmd exports when an
// agent must publish claude.ai artifacts through Argus's artifact_publish tool.
const artifactAccountEnv = "ARGUS_ARTIFACT_ACCOUNT"

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
	// keys dropped by allowlist (see residentSandbox), carried inline.
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
// underArgusSandbox gates the sandbox carry (see residentSandbox).
func readResidentUserSettings(task *model.Task, cfg config.Config, underArgusSandbox bool) residentUserSettings {
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
	us.Sandbox = residentSandbox(doc.Sandbox, underArgusSandbox)
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

// residentLaunchDirName is the daemon/supervisor-owned directory (under the
// Argus data dir) holding the resident's launch-time files.
const residentLaunchDirName = "resident-launch"

// residentLaunchDir returns ~/.argus/resident-launch, created 0700. It must stay
// OUTSIDE every sandbox write grant: ~/.argus/cache is writable by every
// sandboxed agent, and the resident's own worktree (~/.argus/resident) is its
// own write grant, so a file there could be rewritten by the very agent whose
// policy it carries. ~/.argus/routing is placed the same way (data dir root, no
// grant). Never a subpath of ~/.argus/cache or db.ResidentWorkDir().
func residentLaunchDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("no home dir: %w", err)
	}
	dir := filepath.Join(home, ".argus", residentLaunchDirName)
	if err := os.MkdirAll(filepath.Dir(dir), 0o755); err != nil {
		return "", err
	}
	if err := ensurePrivateDir(dir); err != nil {
		return "", err
	}
	return dir, nil
}

// ensurePrivateDir makes path a real (non-symlink) 0700 directory. Lstat, not
// Stat: a symlink or file planted at path is removed and replaced, never
// followed.
func ensurePrivateDir(path string) error {
	fi, err := os.Lstat(path)
	switch {
	case err == nil && fi.IsDir():
		return chmodPrivate(path, fi)
	case err == nil:
		if rerr := os.Remove(path); rerr != nil && !errors.Is(rerr, fs.ErrNotExist) {
			return rerr
		}
	case !errors.Is(err, fs.ErrNotExist):
		return err
	}
	if err := os.Mkdir(path, 0o700); err != nil {
		if !errors.Is(err, fs.ErrExist) {
			return err
		}
		// A concurrent first launch created it between our Lstat and Mkdir:
		// accept a real directory, never a symlink or file raced into place.
		fi, lerr := os.Lstat(path)
		if lerr != nil || !fi.IsDir() {
			return err
		}
		return chmodPrivate(path, fi)
	}
	return os.Chmod(path, 0o700) //nolint:gosec // a directory; 0700 is the private mode
}

func chmodPrivate(path string, fi fs.FileInfo) error {
	if fi.Mode().Perm() != 0o700 {
		return os.Chmod(path, 0o700) //nolint:gosec // a directory; 0700 is the private mode
	}
	return nil
}

// materializeResidentPrompt writes routingPath's content (when set) followed by
// the user CLAUDE.md to <resident-launch>/system-prompt.md (a fixed name, so
// nothing accumulates) and returns its path. The file is verified on every
// launch and atomically rewritten when its bytes differ or it is not a regular
// file.
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
	root, err := residentLaunchDir()
	if err != nil {
		return "", err
	}
	file := filepath.Join(root, "system-prompt.md")
	// Lstat so a planted symlink is never read through; anything but a regular
	// file with the exact bytes is replaced by rename (which never writes through).
	if fi, lerr := os.Lstat(file); lerr == nil && fi.Mode().IsRegular() {
		if got, rerr := os.ReadFile(file); rerr == nil && bytes.Equal(got, buf) {
			return file, nil
		}
		uxlog.Log("[resident] system prompt file differed from expected; restoring %s", file)
		slog.Warn("[resident] system prompt file differed; restoring", "path", file)
	}
	if err := atomicWriteFile(file, buf); err != nil {
		return "", fmt.Errorf("write resident prompt: %w", err)
	}
	return file, nil
}

// atomicWriteFile writes data (mode 0600) to an O_EXCL temp file beside path
// and renames it over path. A rename replaces a planted symlink or file rather
// than writing through it.
func atomicWriteFile(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-") // O_EXCL, 0600
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
// in the private resident-launch dir, keyed by the source path so accounts
// never share one (bounded by the number of accounts).
func ensureResidentUserSkillsDir(task *model.Task, cfg config.Config) string {
	dir := residentAccountDir(task, cfg)
	if dir == "" {
		return ""
	}
	src := filepath.Join(dir, "skills")
	if fi, err := os.Stat(src); err != nil || !fi.IsDir() {
		return ""
	}
	launch, err := residentLaunchDir()
	if err != nil {
		uxlog.Log("[resident] launch dir failed (skipping user skills): %v", err)
		slog.Warn("[resident] launch dir failed", "err", err)
		return ""
	}
	sum := sha256.Sum256([]byte(src))
	root := filepath.Join(launch, "skills", hex.EncodeToString(sum[:])[:16])
	claudeDir := filepath.Join(root, ".claude")
	link := filepath.Join(claudeDir, "skills")
	for _, d := range []string{filepath.Join(launch, "skills"), root, claudeDir} {
		if err := ensurePrivateDir(d); err != nil {
			uxlog.Log("[resident] user skills link dir failed (skipping user skills): %v", err)
			slog.Warn("[resident] user skills link dir failed", "err", err)
			return ""
		}
	}
	if cur, err := os.Readlink(link); err == nil && cur == src {
		return root
	}
	// Whatever sits at link is replaced, never followed. A real directory or
	// file is removed first; a symlink is swapped by atomic rename of a fresh
	// temp link, so concurrent first launches never observe the link missing.
	if fi, err := os.Lstat(link); err == nil && fi.Mode()&os.ModeSymlink == 0 {
		if err := os.RemoveAll(link); err != nil {
			uxlog.Log("[resident] user skills link failed (skipping user skills): %v", err)
			slog.Warn("[resident] user skills link failed", "err", err)
			return ""
		}
	}
	if err := replaceSymlink(src, link); err != nil {
		if cur, rerr := os.Readlink(link); rerr == nil && cur == src {
			return root // a concurrent launch linked the same source
		}
		uxlog.Log("[resident] user skills link failed (skipping user skills): %v", err)
		slog.Warn("[resident] user skills link failed", "err", err)
		return ""
	}
	return root
}

// replaceSymlink atomically points link at src: a uniquely named temp symlink
// beside it is renamed over link (rename replaces a symlink, never follows it).
func replaceSymlink(src, link string) error {
	dir := filepath.Dir(link)
	for range 8 {
		tmp := filepath.Join(dir, fmt.Sprintf(".tmp-link-%d", rand.Int64())) //nolint:gosec // unique temp name, not security-sensitive
		if err := os.Symlink(src, tmp); err != nil {
			if errors.Is(err, fs.ErrExist) {
				continue
			}
			return err
		}
		if err := os.Rename(tmp, link); err != nil {
			_ = os.Remove(tmp)
			return err
		}
		return nil
	}
	return fmt.Errorf("no free temp link name in %s", dir)
}

// residentSandbox carries the user's Claude `sandbox` block for its
// restrictions only, by ALLOWLIST: the block is not restrict-only as written
// (allowAppleEvents, filesystem.disabled, enableWeakerNetworkIsolation,
// allowNestedUserNamespaces, network.allow*, credentials.allowPlaintextInject,
// excludedCommands and any future key can all loosen it), so only the keys
// known to restrict are copied and everything else is dropped, including keys
// this binary has never heard of. The carried set is `enabled`,
// `failIfUnavailable`, `network.deniedDomains`, `filesystem.denyRead` and
// `filesystem.denyWrite`. credentials.* is deliberately omitted: its mask mode
// injects real credentials at hosts, so it is not provably restrict-only.
// autoAllowBashIfSandboxed and allowUnsandboxedCommands are forced false on
// whatever is carried, so every Bash call still prompts.
//
// underArgusSandbox: when the resident itself runs under Argus's sandbox-exec,
// Claude's own Bash sandbox cannot nest inside it (Bash would fail on every
// call once allowUnsandboxedCommands is false), so enabled and
// failIfUnavailable are not carried; the deny lists still are. A missing or
// non-object block, or one with no allowlisted key left, yields nil.
func residentSandbox(raw json.RawMessage, underArgusSandbox bool) json.RawMessage {
	var in map[string]any
	if s := bytes.TrimSpace(raw); len(s) == 0 || s[0] != '{' || json.Unmarshal(s, &in) != nil {
		return nil
	}
	out := map[string]any{}
	if !underArgusSandbox {
		for _, k := range []string{"enabled", "failIfUnavailable"} {
			// Only a true value restricts; false (or a non-bool) carries nothing.
			if v, ok := in[k].(bool); ok && v {
				out[k] = true
			}
		}
	}
	if list := sandboxStringList(in, "network", "deniedDomains"); list != nil {
		out["network"] = map[string]any{"deniedDomains": list}
	}
	fs := map[string]any{}
	for _, k := range []string{"denyRead", "denyWrite"} {
		if list := sandboxStringList(in, "filesystem", k); list != nil {
			fs[k] = list
		}
	}
	if len(fs) > 0 {
		out["filesystem"] = fs
	}
	if len(out) == 0 {
		return nil
	}
	out["autoAllowBashIfSandboxed"] = false
	out["allowUnsandboxedCommands"] = false
	b, err := json.Marshal(out)
	if err != nil {
		return nil
	}
	return b
}

// sandboxStringList returns the non-empty string entries of in[group][key], or
// nil when absent, mistyped or empty.
func sandboxStringList(in map[string]any, group, key string) []string {
	g, _ := in[group].(map[string]any)
	arr, _ := g[key].([]any)
	var out []string
	for _, v := range arr {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			out = append(out, s)
		}
	}
	return out
}
