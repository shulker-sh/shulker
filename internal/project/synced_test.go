package project

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
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

func TestBuildDirsStartsWithTheBuildDirAndSkipsWhatIsGone(t *testing.T) {
	p, linked, detached := syncedFixture(t)
	buildDir := filepath.Join(p.Dir, "build", "client")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		t.Fatal(err)
	}
	server := filepath.Join(filepath.Dir(p.Dir), "server")
	if err := os.MkdirAll(server, 0o755); err != nil {
		t.Fatal(err)
	}
	for dir, side := range map[string]string{server: "server", linked: "client"} {
		f := instance.New()
		f.Source, f.Side = p.Dir, side
		if err := f.Save(dir); err != nil {
			t.Fatal(err)
		}
	}
	registry := []config.Instance{
		{ID: "gone", Launcher: "prism", Dir: filepath.Join(p.Dir, "..", "gone"), Source: p.Dir},
		{ID: "pack-client", Launcher: "prism", Dir: linked, Source: p.Dir},
		{ID: "pack-server", Launcher: "prism", Dir: server, Source: p.Dir},
	}
	lf := &local.File{SyncDirs: map[string][]string{"client": {detached, linked}}}
	got, dirs, err := BuildDirs(p, registry, lf, "client")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{buildDir, linked, detached}; got != buildDir || !slices.Equal(dirs, want) {
		t.Fatalf("build dir %q, dirs %v, want %v", got, dirs, want)
	}
}

func TestSyncTargetTellsTheOwnBuildFromASyncedDirectory(t *testing.T) {
	root := t.TempDir()
	m := &manifest.Manifest{Client: &manifest.Client{}}
	own := filepath.Join(root, m.BuildDir("client"))

	dir, ownBuild, syncedDir, err := SyncTarget(m, root, "client", "", "")
	if err != nil || dir != own || !ownBuild || syncedDir {
		t.Fatalf("no --into is the project's own build: %q own=%v synced=%v err=%v", dir, ownBuild, syncedDir, err)
	}
	if dir, ownBuild, syncedDir, err := SyncTarget(m, root, "client", own, ""); err != nil || dir != own || !ownBuild || syncedDir {
		t.Fatalf("--into the build dir is still the own build: %q own=%v synced=%v err=%v", dir, ownBuild, syncedDir, err)
	}
	elsewhere := filepath.Join(root, "game")
	if dir, ownBuild, syncedDir, err := SyncTarget(m, root, "client", elsewhere, "pack.git"); err != nil || dir != elsewhere || ownBuild || !syncedDir {
		t.Fatalf("--into elsewhere is a synced directory: %q own=%v synced=%v err=%v", dir, ownBuild, syncedDir, err)
	}
	if _, _, _, err := SyncTarget(m, root, "client", "", "pack.git"); out.CodeOf(err) != "into-required" {
		t.Fatalf("a remote source needs --into, got %v", err)
	}
}
