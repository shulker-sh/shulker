package resolve

import (
	"slices"
	"testing"

	"shulker.sh/shulker/internal/env/envtest"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/provider"
)

func TestTheLockRecordsWhereAModsSideCameFrom(t *testing.T) {
	c := envtest.NewCDN(t)
	alpha := envtest.NewHost(c, "alpha")
	alpha.Publish(mod("a-fapi", "fabric-api"), provider.Version{Number: "1.0.0", File: provider.File{Filename: "fabric-api.jar"}}, modJar(t, "fabric-api", "1.0.0", "*"))
	alpha.Publish(mod("a-sodium", "sodium"), provider.Version{Number: "1.0.0", File: provider.File{Filename: "sodium.jar"}}, modJar(t, "sodium", "1.0.0", "client"))
	sided := mod("a-lith", "lithium")
	sided.Side = "server"
	alpha.Publish(sided, provider.Version{Number: "1.0.0", File: provider.File{Filename: "lithium.jar"}}, modJar(t, "lithium", "1.0.0", "*"))
	alpha.Publish(mod("a-zoom", "zoom"), provider.Version{Number: "1.0.0", File: provider.File{Filename: "zoom.jar"}}, modJar(t, "zoom", "1.0.0", "client"))
	h := newHarness(t, alpha)
	before := h.r.Snapshot()
	for _, slug := range []string{"fabric-api", "sodium", "lithium"} {
		h.mustAdd(slug, AddOptions{})
	}
	h.mustAdd("zoom", AddOptions{Side: "both"})

	for id, want := range map[string][2]string{"fabric-api": {"both", ""}, "sodium": {"client", "jar"}, "lithium": {"server", "provider"}, "zoom": {"both", "requires"}} {
		if m := h.mod(id); m.Side != want[0] || m.SideFrom != want[1] {
			t.Errorf("%s side %q from %q, want %q from %q", id, m.Side, m.SideFrom, want[0], want[1])
		}
	}
	if notes := h.r.Changes(before).DependencySides(h.r.Lock.Mods); len(notes) != 0 {
		t.Fatalf("notes %q for sides the jars declare", notes)
	}
}

func TestASideReadFromDependenciesIsNotedWhenItChanges(t *testing.T) {
	mods := map[string]lock.Mod{
		"sodium": {Side: "client", SideFrom: "dependencies"},
		"iris":   {Side: "client", SideFrom: "dependencies"},
		"oculus": {Side: "client", SideFrom: "dependencies"},
		"zoom":   {Side: "client", SideFrom: "jar"},
	}
	c := &Changes{
		Added:   []AddedMod{{ID: "sodium", Side: "client"}, {ID: "zoom", Side: "client"}},
		Updated: []Change{{ID: "iris", FromSide: "both", ToSide: "client"}, {ID: "oculus", From: "1", To: "2"}},
	}
	want := []string{
		"sodium is client-only: every dependency in its mods.toml is CLIENT; shulker set requires.sodium.side both places it on the server too",
		"iris is client-only: every dependency in its mods.toml is CLIENT; shulker set requires.iris.side both places it on the server too",
	}
	if notes := c.DependencySides(mods); !slices.Equal(notes, want) {
		t.Fatalf("notes %q, want %q", notes, want)
	}
}
