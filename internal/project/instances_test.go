package project

import (
	"os"
	"path/filepath"
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
	want := []string{"/s", "/p1", "/p2", "/u", "/x"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("order = %v, want %v", got, want)
		}
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
