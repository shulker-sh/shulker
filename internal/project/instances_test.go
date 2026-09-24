package project

import (
	"testing"

	"shulker.sh/shulker/internal/config"
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
