package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/argus-foundry/argus/internal/agent"
	"github.com/argus-foundry/argus/internal/artifacts"
	"github.com/argus-foundry/argus/internal/claudeartifacts"
	"github.com/argus-foundry/argus/internal/model"
)

// ArtifactStore persists registered-artifact manifest rows. *db.DB satisfies
// it. Kept as an interface so the mcp package doesn't depend on db directly,
// mirroring ClipboardSetter / MessageStore.
type ArtifactStore interface {
	UpsertArtifact(a *model.Artifact) (*model.Artifact, error)
	GetArtifact(taskID, filename string) (*model.Artifact, error)
	PruneFolderArtifacts(taskID, folder string, keep []string) ([]string, error)
}

// SetArtifactManager wires artifact registration. When set (and task
// management is also wired so cwd/id resolution works), the server exposes the
// artifact_register tool. Must be called before ListenAndServe.
func (s *Server) SetArtifactManager(store ArtifactStore) {
	s.artifacts = store
}

// artifactsEnabled reports whether artifact registration is available. Requires
// both the store and task management (the tool resolves the caller's task by
// id/cwd, which needs the task DB).
func (s *Server) artifactsEnabled() bool {
	return s.artifacts != nil && s.taskMgmtEnabled()
}

// artifactToolDefs are exposed only when SetArtifactManager has been called.
var artifactToolDefs = []Tool{
	{
		Name: "artifact_register",
		Description: `Register a file you produced (an HTML report, PDF, rendered markdown, image, text, or audio/video clip) so the user can VIEW it — rendered, not just downloaded — in Argus Web, including on mobile over Tailscale.

Argus copies the file into durable per-task storage at registration time, so write it inside your worktree or a temp dir (e.g. /tmp) and register that path; the original is not tracked and may be deleted afterward. Paths outside your task's worktree, temp dirs and extra_write paths, symlinks, and hard-linked files are refused. Re-registering the same filename overwrites the previous copy (last write wins).

The agent process does not know its own task ID, so identify yourself by passing either ` + "`id`" + ` (sub-tasks should use the ` + "`ARGUS_TASK_ID`" + ` env var exported into every worktree) or ` + "`cwd`" + ` (Argus resolves to the task whose worktree the cwd lives under, longest-prefix wins). At least one is required.

Pass a DIRECTORY as path to register the whole folder: every regular file inside (recursively; hidden files and symlinks skipped) is copied and shown in Argus Web as one folder with a thumbnail gallery for images/videos. Limits: ` + fmt.Sprintf("%d files and %d GiB", model.MaxFolderFiles, model.MaxFolderBytes>>30) + ` per folder, plus the per-file caps below. The folder is identified by the directory's name, and re-registering a folder with that name replaces its previous contents.

Guideline: artifacts render best when SELF-CONTAINED — inline all CSS/JS/images, no external CDN references — because the viewer loads them in a sandboxed frame with no network guarantees. Maximum size is 25 MiB for html/markdown/pdf/image/text, 1 GiB for audio/video (these are streamed and scrubbed via Range requests rather than loaded whole).`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":  map[string]interface{}{"type": "string", "description": "Absolute or relative path to the file (e.g. /tmp/coaching-reports/coaching-2026-05-30.html) or directory you produced. Copied into durable storage now."},
				"title": map[string]interface{}{"type": "string", "description": "Optional display title shown in the artifacts list. Defaults to the file's basename. Ignored for directories (the folder shows its directory name)."},
				"type":  map[string]interface{}{"type": "string", "description": "Optional artifact type: html, markdown, pdf, image, text, audio, or video. Inferred from the file extension when omitted. Ignored for directories (each file's type is inferred)."},
				"id":    map[string]interface{}{"type": "string", "description": "Task ID. If omitted, cwd is used to resolve the task."},
				"cwd":   map[string]interface{}{"type": "string", "description": "Working directory inside the task's worktree. Used when id is omitted."},
			},
			"required": []string{"path"},
		},
	},
}

func (s *Server) toolArtifactRegister(id interface{}, args json.RawMessage) *Response {
	if !s.artifactsEnabled() {
		return toolError(id, "artifact registration not configured")
	}

	var p struct {
		Path  string `json:"path"`
		Title string `json:"title"`
		Type  string `json:"type"`
		ID    string `json:"id"`
		Cwd   string `json:"cwd"`
	}
	json.Unmarshal(args, &p) //nolint:errcheck

	if p.Path == "" {
		return toolError(id, "path is required")
	}

	task, err := s.resolveTask(p.ID, p.Cwd)
	if err != nil {
		return toolError(id, err.Error())
	}

	// The daemon is unsandboxed: the path is opened once and confined to the
	// task's own roots (confine.go) before anything else, and file vs folder
	// is decided from that descriptor, so a refused path reveals nothing.
	src, info, real, err := s.confinedOpenAny(task, p.Cwd, p.Path)
	if err != nil {
		return toolError(id, "failed to register artifact: "+err.Error())
	}
	defer src.Close() //nolint:errcheck // read-only
	if info.IsDir() {
		return s.registerArtifactFolder(id, task, p.Path, real)
	}

	stored, err := s.registerArtifactFile(task, p.Path, p.Title, p.Type, src, info)
	if err != nil {
		return toolError(id, err.Error())
	}
	filename, size := stored.Filename, stored.Size

	log.Printf("[mcp] artifact_register ok: id=%s file=%s type=%s bytes=%d", task.ID, filename, stored.Type, size)
	return toolResult(id, fmt.Sprintf("Registered artifact %q (%s, %d bytes) for task %s (%s). View it in Argus TUI → task Artifacts or Argus Web → task → Artifacts.", filename, stored.Type, size, task.ID, task.Name))
}

// registerArtifactFile copies one already-confined file (src, opened from
// path) into task's durable artifact dir and records its manifest row: the
// single-file half of artifact_register. Returned errors are user-facing
// tool-error text.
func (s *Server) registerArtifactFile(task *model.Task, path, title, typ string, src *os.File, info os.FileInfo) (*model.Artifact, error) {
	// Sanitize the destination basename from the source path. This strips any
	// directory components and rejects degenerate names — the stored filename
	// can never contain a path separator or "..".
	filename, err := model.SanitizeArtifactFilename(path)
	if err != nil {
		return nil, fmt.Errorf("invalid artifact path %q: %w", path, err)
	}

	if _, err := model.ValidateArtifactRelPath(filename); err != nil {
		return nil, fmt.Errorf("invalid artifact path %q: reserved or unsafe name", path)
	}

	// Resolve the artifact type: explicit (validated) or inferred from the ext.
	atype := model.ArtifactType(typ)
	if typ == "" {
		atype = model.InferArtifactType(filename)
	} else if !model.ValidArtifactType(atype) {
		return nil, fmt.Errorf("invalid type %q: must be html, markdown, pdf, image, text, audio, or video", typ)
	}

	if maxBytes := model.MaxBytesForType(atype); info.Size() > maxBytes {
		return nil, fmt.Errorf("failed to register artifact: artifact exceeds %d byte cap (got %d)", maxBytes, info.Size())
	}
	return s.recordArtifact(task, filename, title, atype, src)
}

// recordArtifact copies r into task's durable artifact dir as filename and
// records its manifest row, rolling the bytes back if the row fails.
func (s *Server) recordArtifact(task *model.Task, filename, title string, atype model.ArtifactType, r io.Reader) (*model.Artifact, error) {
	size, err := s.copyArtifact(task.ID, r, filename, atype)
	if err != nil {
		log.Printf("[mcp] artifact_register copy failed: id=%s file=%s err=%v", task.ID, filename, err)
		return nil, fmt.Errorf("failed to register artifact: %w", err)
	}

	if title == "" {
		title = filename
	}

	stored, err := s.artifacts.UpsertArtifact(&model.Artifact{
		TaskID:   task.ID,
		Name:     title,
		Filename: filename,
		Type:     atype,
		Size:     size,
	})
	if err != nil {
		// Roll back the copied bytes so a manifest failure doesn't leave an
		// unreferenced (and therefore unservable) file on disk.
		os.Remove(filepath.Join(agent.ArtifactsDir(task.ID), filename)) //nolint:errcheck
		log.Printf("[mcp] artifact_register manifest failed: id=%s err=%v", task.ID, err)
		return nil, fmt.Errorf("failed to record artifact: %w", err)
	}

	return stored, nil
}

// copyArtifact copies src (an already-confined descriptor, or bytes already
// read through one) into the task's durable artifact dir under the sanitized
// filename, enforcing the per-type size cap. The copy is atomic (temp file in
// the same dir + rename) so a partial write or a concurrent serve never
// observes a half-written artifact. Returns the bytes written.
//
// The size cap is enforced with an io.LimitReader at maxBytes+1 (maxBytes
// resolved per-type via model.MaxBytesForType): if the copy reaches that
// ceiling the source exceeds the cap and we reject, removing the temp file.
func (s *Server) copyArtifact(taskID string, src io.Reader, filename string, atype model.ArtifactType) (int64, error) {
	maxBytes := model.MaxBytesForType(atype)

	root := agent.ArtifactsDir(taskID)
	dest := filepath.Join(root, filepath.FromSlash(filename))
	dir := filepath.Dir(dest)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return 0, fmt.Errorf("create artifact dir: %w", err)
	}

	tmp, err := os.CreateTemp(dir, ".artifact-*.tmp")
	if err != nil {
		return 0, fmt.Errorf("create temp: %w", err)
	}
	tmpName := tmp.Name()
	// LimitReader at cap+1 catches a source that grows past the cap between
	// Stat and copy (e.g. a still-being-written file).
	n, copyErr := io.Copy(tmp, io.LimitReader(src, maxBytes+1))
	closeErr := tmp.Close()
	if copyErr != nil {
		os.Remove(tmpName) //nolint:errcheck
		return 0, fmt.Errorf("copy: %w", copyErr)
	}
	if closeErr != nil {
		os.Remove(tmpName) //nolint:errcheck
		return 0, fmt.Errorf("close temp: %w", closeErr)
	}
	if n > maxBytes {
		os.Remove(tmpName) //nolint:errcheck
		return 0, fmt.Errorf("artifact exceeds %d byte cap", maxBytes)
	}

	// os.CreateTemp already created the temp file 0600 and Rename preserves the
	// mode, so no explicit Chmod is needed (a post-rename Chmod would also be a
	// minor TOCTOU on the destination name).
	if err := os.Rename(tmpName, dest); err != nil {
		os.Remove(tmpName) //nolint:errcheck
		return 0, fmt.Errorf("rename into place: %w", err)
	}
	return n, nil
}

// copyConfined opens path through confinedOpen and copies from that same
// descriptor.
func (s *Server) copyConfined(task *model.Task, path, filename string, atype model.ArtifactType) (int64, error) {
	src, info, err := s.confinedOpen(task, "", path)
	if err != nil {
		return 0, err
	}
	defer src.Close() //nolint:errcheck // read-only
	if maxBytes := model.MaxBytesForType(atype); info.Size() > maxBytes {
		return 0, fmt.Errorf("artifact exceeds %d byte cap (got %d)", maxBytes, info.Size())
	}
	return s.copyArtifact(task.ID, src, filename, atype)
}

// folderRegMu serializes folder registrations so a concurrent re-register can't
// prune rows (and delete bytes) another call just upserted.
var folderRegMu sync.Mutex

// maxFolderWalkEntries bounds the directory walk itself: skipped entries
// (hidden, symlinks, oversized) don't count toward MaxFolderFiles.
const maxFolderWalkEntries = 20000

// registerArtifactFolder copies every regular, non-hidden file under srcDir
// into <artifacts>/<folder>/<rel path> and registers one manifest row per file
// tagged with the folder name. Symlinks and dotfiles are skipped (a symlink
// could point outside the tree the agent meant to share). Rows for files that
// were in a previous registration of the same folder but are gone from srcDir
// are pruned so a re-register replaces the folder's contents.
//
// srcDir is the name the agent gave (it names the folder); walkRoot is the
// confined, descriptor-derived location the caller already checked. Each
// member is confined again as it is opened.
func (s *Server) registerArtifactFolder(id interface{}, task *model.Task, srcDir, walkRoot string) *Response {
	folderRegMu.Lock()
	defer folderRegMu.Unlock()

	folder, err := model.SanitizeArtifactFilename(srcDir)
	if err != nil || folder == model.ArtifactThumbDir || strings.HasPrefix(folder, ".") {
		return toolError(id, fmt.Sprintf("invalid folder path %q", srcDir))
	}

	type entry struct {
		abs, rel string
		size     int64
	}
	var files []entry
	var total int64
	skipped := 0
	visited := 0
	walkErr := filepath.WalkDir(walkRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == walkRoot {
			return nil
		}
		if visited++; visited > maxFolderWalkEntries {
			return fmt.Errorf("folder has more than %d entries", maxFolderWalkEntries)
		}
		if strings.HasPrefix(d.Name(), ".") {
			skipped++
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() {
			skipped++ // symlinks, sockets, devices
			return nil
		}
		rel, err := filepath.Rel(walkRoot, path)
		if err != nil {
			skipped++
			return nil
		}
		rel = filepath.ToSlash(rel)
		if _, err := model.ValidateArtifactRelPath(rel); err != nil || info.Size() > model.MaxBytesForType(model.InferArtifactType(rel)) {
			skipped++
			return nil
		}
		if len(files) >= model.MaxFolderFiles {
			return fmt.Errorf("folder has more than %d files", model.MaxFolderFiles)
		}
		total += info.Size()
		if total > model.MaxFolderBytes {
			return fmt.Errorf("folder exceeds %d byte cap", int64(model.MaxFolderBytes))
		}
		files = append(files, entry{path, rel, info.Size()})
		return nil
	})
	if walkErr != nil {
		return toolError(id, fmt.Sprintf("Failed to read folder %q: %v", srcDir, walkErr))
	}
	if len(files) == 0 {
		return toolError(id, fmt.Sprintf("folder %q has no registrable files", srcDir))
	}

	var keep []string
	failed := 0
	var bytesCopied int64
	for _, e := range files {
		filename := folder + "/" + e.rel
		atype := model.InferArtifactType(e.rel)
		n, err := s.copyConfined(task, e.abs, filename, atype)
		if err != nil {
			log.Printf("[mcp] artifact_register folder copy failed: id=%s file=%s err=%v", task.ID, filename, err)
			failed++
			continue
		}
		if _, err := s.artifacts.UpsertArtifact(&model.Artifact{
			TaskID:   task.ID,
			Name:     e.rel,
			Filename: filename,
			Folder:   folder,
			Type:     atype,
			Size:     n,
		}); err != nil {
			os.Remove(filepath.Join(agent.ArtifactsDir(task.ID), filepath.FromSlash(filename))) //nolint:errcheck
			log.Printf("[mcp] artifact_register folder manifest failed: id=%s file=%s err=%v", task.ID, filename, err)
			failed++
			continue
		}
		keep = append(keep, filename)
		bytesCopied += n
	}
	if len(keep) == 0 {
		return toolError(id, fmt.Sprintf("Failed to register folder %q: all %d files failed", srcDir, len(files)))
	}

	pruned := 0
	if stale, err := s.artifacts.PruneFolderArtifacts(task.ID, folder, keep); err != nil {
		log.Printf("[mcp] artifact_register folder prune failed: id=%s folder=%s err=%v", task.ID, folder, err)
	} else {
		pruned = len(stale)
		for _, fn := range stale {
			os.Remove(filepath.Join(agent.ArtifactsDir(task.ID), filepath.FromSlash(fn))) //nolint:errcheck
		}
	}

	log.Printf("[mcp] artifact_register folder ok: id=%s folder=%s files=%d failed=%d skipped=%d pruned=%d bytes=%d", task.ID, folder, len(keep), failed, skipped, pruned, bytesCopied)
	note := ""
	if pruned > 0 {
		note = fmt.Sprintf(" Replaced %d previously registered file(s) in folder %q that are no longer in the source directory.", pruned, folder)
	}
	return toolResult(id, fmt.Sprintf("Registered folder %q (%d files, %d bytes; %d skipped, %d failed) for task %s (%s). View it in Argus Web → task → Artifacts as a gallery.%s", folder, len(keep), bytesCopied, skipped, failed, task.ID, task.Name, note))
}

// ArtifactPublisher publishes a page to claude.ai as a named account and,
// when that account is unavailable, says which other accounts can publish.
// *claudeartifacts.Publisher satisfies it; the daemon resolves account tokens
// behind it, so no token ever reaches this package's results or logs.
type ArtifactPublisher interface {
	Publish(ctx context.Context, account string, in claudeartifacts.PublishInput) (claudeartifacts.Result, error)
	AvailableAccounts(ctx context.Context, exclude string) []string
}

// SetArtifactPublisher wires claude.ai publishing. When set, the server exposes
// artifact_publish. Must be called before ListenAndServe.
func (s *Server) SetArtifactPublisher(p ArtifactPublisher) {
	s.publisher = p
}

// artifactPublishTimeout bounds one artifact_publish call, including the
// concurrent probes on the unavailable path.
const artifactPublishTimeout = 2 * time.Minute

// artifactOpenGOOS is the platform artifact_open gates on; a variable so tests
// can exercise the not-supported branch.
var artifactOpenGOOS = runtime.GOOS

// openArgs builds the macOS `open` arguments: -R reveals in Finder.
func openArgs(path string, reveal bool) []string {
	if reveal {
		return []string{"-R", path}
	}
	return []string{path}
}

// defaultArtifactOpener runs macOS `open` on a verified artifact path.
func defaultArtifactOpener(path string, reveal bool) error {
	out, err := exec.Command("open", openArgs(path, reveal)...).CombinedOutput() //nolint:gosec // G204: fixed binary; path is a manifest-registered file resolved inside the task's artifact dir.
	if err != nil {
		return fmt.Errorf("open: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// artifactPublishToolDefs are exposed only when SetArtifactPublisher has been
// called.
var artifactPublishToolDefs = []Tool{
	{
		Name: "artifact_publish",
		Description: `Publish a single self-contained HTML page to claude.ai as an artifact on a named account, and get its claude.ai link back. Argus publishes it from the daemon with that account's configured token, so it works whichever account this session runs on.

Use it when ` + "`ARGUS_ARTIFACT_ACCOUNT`" + ` is set: pass ` + "`account`" + ` = its value. To publish a new version of an existing artifact, also pass its link as ` + "`update`" + `.

The file must be .html or .htm, at most 16 MiB, start with <!DOCTYPE html> or <html>, and live in your worktree or a temp dir (not a symlink). Inline all CSS, JS and images.

The result is JSON. ` + "`status`" + ` is "published" (with ` + "`url`" + `) or "unavailable": the account cannot publish right now. An unavailable result names the page's saved copy as ` + "`artifact`" + ` (a registered task artifact) and ` + "`artifact_path`" + ` (its absolute path) and lists ` + "`available_accounts`" + ` that can publish instead. On an unavailable result ask the user what to do; do not publish elsewhere on your own.

Identify your task with ` + "`id`" + ` (the ` + "`ARGUS_TASK_ID`" + ` env var) or ` + "`cwd`" + `, so an unavailable page can be saved to it.`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string", "description": "Path to the .html/.htm file to publish."},
				"account": map[string]interface{}{"type": "string", "description": "Account to publish as, e.g. the value of ARGUS_ARTIFACT_ACCOUNT."},
				"title":   map[string]interface{}{"type": "string", "description": "Artifact title. Defaults to the file name for a new artifact; an update keeps the current title unless one is given."},
				"update":  map[string]interface{}{"type": "string", "description": "Link of an existing artifact (https://claude.ai/artifact/<slug>) to publish a new version of."},
				"id":      map[string]interface{}{"type": "string", "description": "Task ID. If omitted, cwd is used to resolve the task."},
				"cwd":     map[string]interface{}{"type": "string", "description": "Working directory inside the task's worktree. Used when id is omitted."},
			},
			"required": []string{"path", "account"},
		},
	},
}

// artifactOpenToolDefs are exposed with artifact_register.
var artifactOpenToolDefs = []Tool{
	{
		Name:        "artifact_open",
		Description: `Open one of your task's registered artifacts on the user's Mac: in its default application (an HTML page opens in the browser), or, with ` + "`reveal: true`" + `, selected in a Finder window. Only files registered to the calling task (artifact_register, or the saved copy an unavailable artifact_publish reports) can be opened. macOS only. Identify your task with ` + "`id`" + ` or ` + "`cwd`" + `.`,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"filename": map[string]interface{}{"type": "string", "description": "The registered artifact's file name (e.g. report.html)."},
				"reveal":   map[string]interface{}{"type": "boolean", "description": "Reveal the file in Finder instead of opening it."},
				"id":       map[string]interface{}{"type": "string", "description": "Task ID. If omitted, cwd is used to resolve the task."},
				"cwd":      map[string]interface{}{"type": "string", "description": "Working directory inside the task's worktree. Used when id is omitted."},
			},
			"required": []string{"filename"},
		},
	},
}

// artifactPublished is artifact_publish's success result.
type artifactPublished struct {
	Status  string `json:"status"`
	Account string `json:"account"`
	URL     string `json:"url"`
	Slug    string `json:"slug"`
	Version string `json:"version,omitempty"`
	Title   string `json:"title,omitempty"`
}

// artifactUnavailable is artifact_publish's result when the account cannot
// publish. It is a result, not a tool error: the agent is meant to act on it.
type artifactUnavailable struct {
	Status   string `json:"status"`
	Account  string `json:"account"`
	Artifact string `json:"artifact,omitempty"`
	// ArtifactPath is the saved copy's absolute path, so the page can be
	// published later with `argus artifact publish <artifact_path>`.
	ArtifactPath string `json:"artifact_path,omitempty"`
	// Update echoes the call's update link, for that later command.
	Update            string   `json:"update,omitempty"`
	AvailableAccounts []string `json:"available_accounts"`
	Message           string   `json:"message"`
}

func jsonToolResult(id interface{}, v any) *Response {
	b, err := json.Marshal(v)
	if err != nil {
		return toolError(id, fmt.Sprintf("encode result: %v", err))
	}
	return toolResult(id, string(b))
}

func (s *Server) toolArtifactPublish(id interface{}, args json.RawMessage) *Response {
	if s.publisher == nil {
		return toolError(id, "artifact publishing not configured")
	}
	var p struct {
		Path    string `json:"path"`
		Account string `json:"account"`
		Title   string `json:"title"`
		Update  string `json:"update"`
		ID      string `json:"id"`
		Cwd     string `json:"cwd"`
	}
	json.Unmarshal(args, &p) //nolint:errcheck
	if p.Path == "" {
		return toolError(id, "path is required")
	}
	if p.Account == "" {
		return toolError(id, "account is required")
	}

	// The daemon reads the file for the agent, so it must know whose sandbox
	// to confine that read to (design D7).
	if !s.taskMgmtEnabled() || (p.ID == "" && p.Cwd == "") {
		return toolError(id, "artifact_publish needs the calling task: pass id (ARGUS_TASK_ID) or cwd")
	}
	task, err := s.resolveTask(p.ID, p.Cwd)
	if err != nil {
		return toolError(id, err.Error())
	}
	// Cheap name check first, then a confined read (confine.go) from one
	// descriptor, then the content checks.
	if err := claudeartifacts.ValidateHTML(p.Path, []byte("<html>")); err != nil {
		return toolError(id, err.Error())
	}
	content, err := s.confinedRead(task, p.Cwd, p.Path, claudeartifacts.MaxContentBytes)
	if err == nil {
		err = claudeartifacts.ValidateHTML(p.Path, content)
	}
	if err != nil {
		log.Printf("[artifacts] publish refused: id=%s account=%s err=%v", task.ID, p.Account, err)
		return toolError(id, err.Error())
	}

	ctx, cancel := context.WithTimeout(s.shutdownCtx, artifactPublishTimeout)
	defer cancel()
	res, err := s.publisher.Publish(ctx, p.Account, claudeartifacts.PublishInput{
		Name: p.Path, Content: content, Title: p.Title, UpdateURL: p.Update,
	})
	switch {
	case err == nil:
		log.Printf("[artifacts] publish ok: account=%s slug=%s version=%s update=%t", p.Account, res.Slug, res.Version, p.Update != "")
		return jsonToolResult(id, artifactPublished{
			Status: "published", Account: p.Account, URL: res.URL, Slug: res.Slug, Version: res.Version, Title: res.Title,
		})
	case errors.Is(err, claudeartifacts.ErrAccountUnavailable):
		return s.artifactPublishUnavailable(ctx, id, task, p.Account, p.Path, p.Title, p.Update, content)
	default:
		log.Printf("[artifacts] publish failed: account=%s err=%v", p.Account, err)
		return toolError(id, err.Error())
	}
}

// artifactPublishUnavailable handles an unavailable account: it saves the
// page as an artifact of the calling task (when one resolves), probes the
// other configured accounts, and reports both.
func (s *Server) artifactPublishUnavailable(ctx context.Context, id interface{}, task *model.Task, account, path, title, update string, content []byte) *Response {
	result := artifactUnavailable{Status: "unavailable", Account: account, Update: update}
	saved := ""
	if s.artifacts != nil {
		// Store the bytes already read through the confined descriptor; the
		// path is never reopened.
		stored, err := s.recordPublishedPage(task, path, title, content)
		if err != nil {
			log.Printf("[artifacts] publish unavailable: save failed: id=%s err=%v", task.ID, err)
			saved = fmt.Sprintf(" Saving it as a task artifact failed (%v).", err)
		} else {
			result.Artifact = stored.Filename
			result.ArtifactPath = filepath.Join(agent.ArtifactsDir(task.ID), filepath.FromSlash(stored.Filename))
			saved = fmt.Sprintf(" The page is saved as task artifact %q at %s.", stored.Filename, result.ArtifactPath)
		}
	}
	result.AvailableAccounts = s.publisher.AvailableAccounts(ctx, account)
	if result.AvailableAccounts == nil {
		result.AvailableAccounts = []string{}
	}
	others := "No other configured account can publish right now."
	if len(result.AvailableAccounts) > 0 {
		others = "Accounts that can publish: " + strings.Join(result.AvailableAccounts, ", ") + "."
	}
	result.Message = fmt.Sprintf("Account %q cannot publish artifacts right now (its artifact tools are unavailable, usually because its usage is exhausted).%s %s Ask the user how to proceed.", account, saved, others)
	log.Printf("[artifacts] publish unavailable: account=%s artifact=%q available=%v", account, result.Artifact, result.AvailableAccounts)
	return jsonToolResult(id, result)
}

// recordPublishedPage saves an unpublished page's bytes as a task artifact
// named after path.
func (s *Server) recordPublishedPage(task *model.Task, path, title string, content []byte) (*model.Artifact, error) {
	filename, err := model.SanitizeArtifactFilename(path)
	if err != nil {
		return nil, fmt.Errorf("invalid artifact path %q: %w", path, err)
	}
	if _, err := model.ValidateArtifactRelPath(filename); err != nil {
		return nil, fmt.Errorf("invalid artifact path %q: reserved or unsafe name", path)
	}
	return s.recordArtifact(task, filename, title, model.ArtifactHTML, bytes.NewReader(content))
}

// openableExtensions is artifact_open's allowlist for a non-reveal open: a
// registered artifact opens in its default app only when its stored type is
// listed AND its extension is one of that type's. Everything else (scripts,
// .command, .terminal, .webloc, .scpt, …, which `open` would run outside the
// agent's sandbox) can only be revealed.
var openableExtensions = map[model.ArtifactType][]string{
	model.ArtifactHTML:     {".html", ".htm"},
	model.ArtifactPDF:      {".pdf"},
	model.ArtifactImage:    {".png", ".jpg", ".jpeg", ".gif", ".webp", ".svg"},
	model.ArtifactMarkdown: {".md", ".markdown"},
	model.ArtifactText:     {".txt", ".log"},
	model.ArtifactAudio:    {".mp3", ".m4a", ".wav"},
	model.ArtifactVideo:    {".mp4", ".mov"},
}

// bundleExtensions are macOS bundle/package suffixes artifact_open never
// touches, even to reveal.
var bundleExtensions = []string{".app", ".appex", ".bundle", ".framework", ".kext", ".mpkg", ".pkg", ".plugin", ".prefpane", ".saver"}

func openableArtifact(a *model.Artifact) bool {
	return slices.Contains(openableExtensions[a.Type], strings.ToLower(filepath.Ext(a.Filename)))
}

func (s *Server) toolArtifactOpen(id interface{}, args json.RawMessage) *Response {
	if !s.artifactsEnabled() {
		return toolError(id, "artifact registration not configured")
	}
	var p struct {
		Filename string `json:"filename"`
		Reveal   bool   `json:"reveal"`
		ID       string `json:"id"`
		Cwd      string `json:"cwd"`
	}
	json.Unmarshal(args, &p) //nolint:errcheck
	if p.Filename == "" {
		return toolError(id, "filename is required")
	}
	if artifactOpenGOOS != "darwin" {
		return toolError(id, fmt.Sprintf("artifact_open is not supported on %s: it needs macOS", artifactOpenGOOS))
	}
	task, err := s.resolveTask(p.ID, p.Cwd)
	if err != nil {
		return toolError(id, err.Error())
	}
	art, err := s.artifacts.GetArtifact(task.ID, p.Filename)
	if err != nil {
		log.Printf("[artifacts] open lookup failed: id=%s file=%q err=%v", task.ID, p.Filename, err)
		return toolError(id, fmt.Sprintf("look up artifact: %v", err))
	}
	if art == nil {
		log.Printf("[artifacts] open refused (not registered): id=%s file=%q", task.ID, p.Filename)
		return toolError(id, fmt.Sprintf("%q is not a registered artifact of task %s", p.Filename, task.ID))
	}
	full, ok := artifacts.ResolvePath(task.ID, art.Filename)
	if !ok {
		log.Printf("[artifacts] open refused (path escapes): id=%s file=%q", task.ID, art.Filename)
		return toolError(id, fmt.Sprintf("invalid artifact path %q", art.Filename))
	}
	info, err := os.Stat(full)
	if err != nil {
		return toolError(id, fmt.Sprintf("artifact %q has no bytes on disk", art.Filename))
	}
	if info.IsDir() || slices.Contains(bundleExtensions, strings.ToLower(filepath.Ext(full))) {
		log.Printf("[artifacts] open refused (directory or bundle): id=%s file=%q", task.ID, art.Filename)
		return toolError(id, fmt.Sprintf("%q is a directory or app bundle; Argus will not open or reveal it", art.Filename))
	}
	if !p.Reveal && !openableArtifact(art) {
		log.Printf("[artifacts] open refused (type %s not openable): id=%s file=%q", art.Type, task.ID, art.Filename)
		return toolError(id, fmt.Sprintf("%q (type %s) is not a file type Argus opens directly; pass reveal: true to show it in Finder instead", art.Filename, art.Type))
	}
	opener := s.openArtifact
	if opener == nil {
		opener = defaultArtifactOpener
	}
	if err := opener(full, p.Reveal); err != nil {
		log.Printf("[artifacts] open failed: id=%s file=%q reveal=%t err=%v", task.ID, art.Filename, p.Reveal, err)
		return toolError(id, fmt.Sprintf("Failed to open %q: %v", art.Filename, err))
	}
	log.Printf("[artifacts] open ok: id=%s file=%q reveal=%t", task.ID, art.Filename, p.Reveal)
	if p.Reveal {
		return toolResult(id, fmt.Sprintf("Revealed %q in Finder.", art.Filename))
	}
	return toolResult(id, fmt.Sprintf("Opened %q in its default application.", art.Filename))
}
