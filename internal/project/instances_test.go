package project

import (
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
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
