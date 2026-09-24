package project

import (
	"os"
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/manifest"
)

func syncedFixture(t *testing.T) (p *Project, linked, detached string) {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, "pack")
	linked = filepath.Join(root, "prism", "pack", ".minecraft")
	detached = filepath.Join(root, "builds", "pack-client")
	for _, d := range []string{dir, linked, detached} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f := instance.New()
	f.Source, f.Side = dir, "client"
	if err := f.Save(detached); err != nil {
		t.Fatal(err)
	}
	m := &manifest.Manifest{Name: "pack", Client: &manifest.Client{}}
	return &Project{Dir: dir, Manifest: m}, linked, detached
}

func TestSyncedFromListsRowsThenTheLocalFilesDetachedBuilds(t *testing.T) {
	p, linked, detached := syncedFixture(t)
	registry := []config.Instance{
		{ID: "pack-client", Launcher: "prism", Dir: linked, Source: p.Dir},
		{ID: "other", Launcher: "prism", Dir: filepath.Join(p.Dir, "..", "elsewhere"), Source: "https://example.com/other.git"},
		{ID: "self", Dir: p.Dir, Source: p.Dir},
	}
	lf := &local.File{SyncDirs: map[string][]string{"client": {linked, detached, filepath.Join(p.Dir, "gone")}}}
	got, err := SyncedFrom(p, registry, lf)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("entries = %+v", got)
	}
	if got[0].ID != "pack-client" || got[0].Detached {
		t.Fatalf("the row comes first: %+v", got[0])
	}
	d := got[1]
	if !d.Detached || d.ID != "pack-client-2" || d.Dir != detached || d.Source != p.Dir || d.Side != "client" || d.Name != "pack" {
		t.Fatalf("the detached build takes a free id: %+v", d)
	}
}
