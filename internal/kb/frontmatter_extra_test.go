package kb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/drn/argus/internal/testutil"
)

func TestRenderMarkdown_PreservesExtraFrontmatter(t *testing.T) {
	src := "---\ntitle: \"Open loops\"\ntags: [resident]\ndream: skip\nowner: solo\n---\n\nbody\n"
	doc := ParseDocument("resident/open-loops.md", src)
	out := RenderMarkdown(&doc)
	for _, want := range []string{"title: \"Open loops\"", "tags: [resident]", "dream: skip", "owner: solo"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered markdown lost %q:\n%s", want, out)
		}
	}
	// Round-trips stably.
	doc2 := ParseDocument("resident/open-loops.md", out)
	testutil.Equal(t, RenderMarkdown(&doc2), out)
}

func TestParseDocument_ListTagsAreNotExtras(t *testing.T) {
	src := "---\ntitle: T\ntags:\n  - a\n  - b\n---\nbody\n"
	doc := ParseDocument("x.md", src)
	out := RenderMarkdown(&doc)
	if strings.Contains(out, "- a") {
		t.Fatalf("list tag leaked into extras:\n%s", out)
	}
}

func TestSeedVaultFile_CreatesThenNeverOverwrites(t *testing.T) {
	vault := t.TempDir()
	store := &fakeKBStore{}
	created, err := SeedVaultFile(store, vault, "resident/open-loops.md", "---\ntitle: \"Open loops\"\ntags: [resident]\ndream: skip\n---\n\nnone\n")
	testutil.NoError(t, err)
	testutil.Equal(t, created, true)
	abs := filepath.Join(vault, "resident", "open-loops.md")
	data, err := os.ReadFile(abs)
	testutil.NoError(t, err)
	if !strings.Contains(string(data), "dream: skip") {
		t.Fatalf("seeded file lost dream: skip:\n%s", data)
	}
	testutil.Equal(t, len(store.upserts), 1)

	// The user/agent edits the file; a re-seed must leave it alone.
	testutil.NoError(t, os.WriteFile(abs, []byte("EDITED"), 0o644))
	created, err = SeedVaultFile(store, vault, "resident/open-loops.md", "different")
	testutil.NoError(t, err)
	testutil.Equal(t, created, false)
	data, _ = os.ReadFile(abs)
	testutil.Equal(t, string(data), "EDITED")
	testutil.Equal(t, len(store.upserts), 1)
}

func TestSeedVaultFile_RejectsEscapingPaths(t *testing.T) {
	vault := t.TempDir()
	for _, p := range []string{"../evil.md", "/abs.md", "a/../../evil.md", ""} {
		if _, err := SeedVaultFile(&fakeKBStore{}, vault, p, "x"); err == nil {
			t.Fatalf("path %q should be rejected", p)
		}
	}
}

type fakeKBStore struct{ upserts []string }

func (f *fakeKBStore) KBUpsert(d *Document) error { f.upserts = append(f.upserts, d.Path); return nil }
func (f *fakeKBStore) KBDelete(string) error      { return nil }
func (f *fakeKBStore) KBMetadataMap() (map[string]int64, error) {
	return map[string]int64{}, nil
}
