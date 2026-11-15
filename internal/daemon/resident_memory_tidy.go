package daemon

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/argus-foundry/argus/internal/config"
)

func residentMemoryTidyZone() (string, error) {
	return resolveResidentMemoryTidyZone(os.Getenv("TZ"), os.Readlink)
}

func resolveResidentMemoryTidyZone(tz string, readlink func(string) (string, error)) (string, error) {
	valid := func(name string) bool {
		if name == "" || strings.EqualFold(name, "Local") {
			return false
		}
		_, err := time.LoadLocation(name)
		return err == nil
	}
	if valid(tz) {
		return tz, nil
	}
	if target, err := readlink("/etc/localtime"); err == nil {
		target = "/" + target
		if index := strings.LastIndex(target, "/zoneinfo/"); index >= 0 {
			name := target[index+len("/zoneinfo/"):]
			if valid(name) {
				return name, nil
			}
		}
	}
	return "", errors.New("cannot resolve host timezone from TZ or /etc/localtime zoneinfo symlink")
}

func residentMemoryTidyNote(cfg config.Config) string {
	return fmt.Sprintf(`Built-in nightly memory tidy (config-owned; resident.memory_tidy controls this timer).

Curate your own memory folder %s/ now, using only kb_* tools for vault I/O. Read the files and search before editing. This is your own dreaming pass; the shared KB maintenance pass leaves this folder alone.

- Merge duplicate notes and facts. Resolve conflicting facts in favor of the newest dated evidence; preserve uncertainty when you cannot establish which is newest.
- Drop stale or finished open loops, and cancel their personal follow-up timers with resident_timer_cancel. Check task_inbox before replacing any missing timer. Retain unresolved commitments; age alone does not make a promise stale.
- Keep files lean and focused. Fix broken links, updating references only inside the memory folder when merging notes. Keep all working memory inside this folder and preserve title, tags and dream: skip frontmatter.
- NEVER delete Aaron's explicit instructions or preferences. When two explicit preferences conflict, keep the older one with a dated "superseded by …" note pointing to the newer preference. Retain instructions that appear obsolete, and ask Aaron only when his attention is needed.
- Keep a short dated change log in this folder's log/YYYY-MM-DD.md describing what you changed (or that no changes were needed), without copying the whole memory into it.
- Acknowledge this inbox fire after the pass. Stay quiet: do not message Aaron unless something needs his attention. Do not send a routine completion report.

The built-in timer is not an open loop and cannot be cancelled by you; config controls it.`, residentMemoryDir(cfg))
}
