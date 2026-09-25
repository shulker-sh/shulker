package project

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/manifest"
)

func TestSortInstancesOrdersByLauncherThenLabelThenDir(t *testing.T) {
	entries := []InstanceEntry{
		{Instance: config.Instance{ID: "b", Dir: "/x"}},
		{Instance: config.Instance{ID: "z", Launcher: "prism", Dir: "/p2"}},
		{Instance: config.Instance{ID: "a", Launcher: "unknown", Dir: "/u"}},
		{Instance: config.Instance{ID: "z", Name: "Alpha", Launcher: "prism", Dir: "/p1"}},
		{Instance: config.Instance{ID: "s", Launcher: "shulker", Dir: "/s"}},
	}
	SortInstances(entries)
	var got []string
	for _, e := range entries {
		got = append(got, e.Dir)
	}
	if want := []string{"/s", "/p1", "/p2", "/u", "/x"}; !slices.Equal(got, want) {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

func TestInstanceAtReadsTheInstanceFileUnlessItSaysUnlinked(t *testing.T) {
	dir := t.TempDir()
	if _, ok := InstanceAt(dir); ok {
		t.Fatal("an empty directory is no instance")
	}
	f := instance.New()
	f.Source = "https://example.com/pack.git"
	f.EnsureResolved().LastSyncAt = "2026-09-24T10:00:00Z"
	if err := f.Save(dir); err != nil {
		t.Fatal(err)
	}
	in, ok := InstanceAt(dir)
	if !ok || in.Source != f.Source || in.Dir != dir || in.Name != filepath.Base(dir) || in.LastSync != "2026-09-24T10:00:00Z" {
		t.Fatalf("recognised %+v, %v", in, ok)
	}
	f.IsUnlinked = true
	if err := f.Save(dir); err != nil {
		t.Fatal(err)
	}
	if _, ok := InstanceAt(dir); ok {
		t.Fatal("an unlinked instance file wins over its source")
	}
}

func TestRepairIntentWritesADefaultsOnlyFileForAnInPlaceProjectAndKeepsAnUnreadableOne(t *testing.T) {
	dir := t.TempDir()
	rep, err := RepairIntent(config.Instance{Dir: dir})
	if err != nil || rep.Wrote {
		t.Fatalf("a directory with no source gets no file: %+v, %v", rep, err)
	}
	m := &manifest.Manifest{Schema: manifest.SchemaURL, Name: "pack", Version: "1.0", Authors: []string{"me"}, Minecraft: "26.2", Loader: manifest.Loader{Type: "fabric", Version: "0.17.3"}, Client: &manifest.Client{Build: "."}, Requires: map[string]manifest.Require{"src": {Source: "https://example.com/pack.git"}}}
	if err := m.Save(filepath.Join(dir, manifest.FileName)); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, instance.Dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(instance.Path(dir), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err = RepairIntent(config.Instance{Dir: dir})
	if err != nil || !rep.Wrote || rep.Kept != instance.Path(dir)+".replaced" || rep.Unreadable == nil {
		t.Fatalf("repaired %+v, %v", rep, err)
	}
	f, err := instance.Load(dir)
	if err != nil || f.Source != "" || f.Side != "" {
		t.Fatalf("an in-place project's file holds no source: %+v, %v", f, err)
	}
	if rep, err := RepairIntent(config.Instance{Dir: dir}); err != nil || rep.Wrote {
		t.Fatalf("a readable file is left alone: %+v, %v", rep, err)
	}
}

func TestDetachedBuildNeedsASourcedInstanceFileAndNoManifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "My Pack")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, ok := DetachedBuild(dir); ok {
		t.Fatal("an empty directory is no detached build")
	}
	f := instance.New()
	f.Source = "https://example.com/pack.git"
	f.Side = "client"
	if err := f.Save(dir); err != nil {
		t.Fatal(err)
	}
	e, ok := DetachedBuild(dir)
	if !ok || !e.Detached || e.ID != "my-pack" || e.Name != "My Pack" || e.Source != f.Source || e.Side != "client" || e.Status != StatusNotSynced {
		t.Fatalf("entry = %+v, %v", e, ok)
	}
	if err := os.WriteFile(filepath.Join(dir, manifest.FileName), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := DetachedBuild(dir); ok {
		t.Fatal("a directory with a manifest is a project, not a detached build")
	}
}

func TestMatchInstancesPrefersTheIDThenNamesThenTheDirectory(t *testing.T) {
	dir := t.TempDir()
	pool := []InstanceEntry{
		{Instance: config.Instance{ID: "smp", Name: "Pack", Dir: "/a"}},
		{Instance: config.Instance{ID: "pack", Name: "Other", Dir: "/b"}},
		{Instance: config.Instance{ID: "two", Name: "pack", Dir: dir}},
	}
	if got := MatchInstances(pool, ""); len(got) != 3 {
		t.Fatalf("no query keeps the pool: %+v", got)
	}
	if got := MatchInstances(pool, "pack"); len(got) != 1 || got[0].ID != "pack" {
		t.Fatalf("an exact id wins: %+v", got)
	}
	if got := MatchInstances(pool, "PACK"); len(got) != 2 || got[0].ID != "smp" || got[1].ID != "two" {
		t.Fatalf("names match case-insensitively: %+v", got)
	}
	if got := MatchInstances(pool, dir); len(got) != 1 || got[0].ID != "two" {
		t.Fatalf("a directory matches last: %+v", got)
	}
	if got := MatchInstances(pool, "nothing"); len(got) != 0 {
		t.Fatalf("nothing matches: %+v", got)
	}
}

func TestInstanceFilterNarrowsByLauncherAndSide(t *testing.T) {
	entries := []InstanceEntry{
		{Instance: config.Instance{ID: "a", Launcher: "prism"}, Side: "client"},
		{Instance: config.Instance{ID: "b", Launcher: "prism"}, Side: "server"},
		{Instance: config.Instance{ID: "c", Launcher: "shulker"}, Side: "client"},
	}
	ids := func(entries []InstanceEntry) []string {
		var got []string
		for _, e := range entries {
			got = append(got, e.ID)
		}
		return got
	}
	if got := ids(InstanceFilter{}.Narrow(entries)); !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("no filter keeps everything: %v", got)
	}
	if got := ids(InstanceFilter{Launcher: "prism"}.Narrow(entries)); !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("launcher = %v", got)
	}
	if got := ids(InstanceFilter{Side: "client"}.Narrow(entries)); !slices.Equal(got, []string{"a", "c"}) {
		t.Fatalf("side = %v", got)
	}
	if got := ids(InstanceFilter{Launcher: "prism", Side: "client"}.Narrow(entries)); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("both = %v", got)
	}
	if got := (InstanceFilter{Launcher: "prism", Side: "client"}).Narrow(entries[1:]); got != nil {
		t.Fatalf("nothing admitted is nil, not %v", got)
	}
}
