package resolve

import (
	"slices"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
)

func TestSplitLocalFilesKeepsTheOrderGiven(t *testing.T) {
	m := &manifest.Manifest{Requires: map[string]manifest.Require{
		"sodium":  {},
		"private": {File: "files/private.jar"},
		"base":    {File: "files/base.mrpack", Type: manifest.TypeModpack},
	}}

	local, rest := SplitLocalFiles(m, []string{"base", "private", "sodium", "missing"})

	if want := []string{"private"}; !slices.Equal(local, want) {
		t.Fatalf("local = %q, want %q", local, want)
	}
	if want := []string{"base", "sodium", "missing"}; !slices.Equal(rest, want) {
		t.Fatalf("rest = %q, want %q", rest, want)
	}
}

func TestUnshippedWarnsOnlyForAnUnconditionedModOffEverySide(t *testing.T) {
	c := &Changes{Added: []AddedMod{
		{ID: "sodium", Side: "client"},
		{ID: "lithium", Side: "client"},
		{ID: "iris", Side: "client"},
		{ID: "fresh-animations", Side: "client"},
		{ID: "fabric-api", Side: "both"},
	}}
	mods := map[string]lock.Mod{"sodium": {}, "lithium": {RequiredBy: []string{"base"}}, "iris": {}, "fabric-api": {}}
	placements := map[string]build.Placement{"iris": {Feature: manifest.StringList{"shaders"}}}

	got := c.Unshipped([]string{"server"}, mods, placements)

	want := []string{"sodium is client only, so no side of this project ships it; shulker set requires.sodium.side both ships it anyway"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := c.Unshipped(nil, mods, placements); got != nil {
		t.Fatalf("a project with no sides gets no warning: %q", got)
	}
}

func TestIsSideDeclared(t *testing.T) {
	for _, tc := range []struct {
		sides []string
		side  string
		want  bool
	}{
		{[]string{"client"}, "client", true},
		{[]string{"client"}, "server", false},
		{[]string{"client"}, "", true},
		{[]string{"client"}, "both", true},
		{nil, "both", false},
	} {
		if got := IsSideDeclared(tc.sides, tc.side); got != tc.want {
			t.Errorf("IsSideDeclared(%q, %q) = %v, want %v", tc.sides, tc.side, got, tc.want)
		}
	}
}
