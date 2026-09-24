package resolve

import (
	"testing"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

func TestRemoveDropsTheDependenciesNothingElseNeeds(t *testing.T) {
	_, _, h := twoHosts(t)
	h.mustAdd("sodium", AddOptions{})

	err := h.r.Remove([]string{"fabric-api"})
	if e := out.AsError(err); e == nil || e.Code != "not-direct" || len(e.Items) != 1 || e.Items[0] != "sodium" {
		t.Fatalf("removing a dependency: %v", err)
	}
	if err := h.r.Remove([]string{"nope"}); out.CodeOf(err) != "mod-not-found" {
		t.Fatalf("removing an unknown mod: %v", err)
	}

	before := h.r.Snapshot()
	if err := h.r.Remove([]string{"sodium"}); err != nil {
		t.Fatal(err)
	}
	c := h.r.Changes(before)
	if len(c.Removed) != 2 || c.Removed[0].ID != "fabric-api" || c.Removed[0].RequiredBy[0] != "sodium" || c.Removed[1].ID != "sodium" {
		t.Fatalf("removed: %+v", c.Removed)
	}
	if len(h.r.Lock.Mods) != 0 || len(h.r.Manifest.Mods()) != 0 {
		t.Fatalf("lock mods %v, manifest mods %v", h.r.Lock.Mods, h.r.Manifest.Mods())
	}

	h.mustAdd("sodium", AddOptions{})
	h.mustAdd("fabric-api", AddOptions{})
	before = h.r.Snapshot()
	if err := h.r.Remove([]string{"sodium"}); err != nil {
		t.Fatal(err)
	}
	if c := h.r.Changes(before); len(c.Removed) != 1 || c.Removed[0].ID != "sodium" {
		t.Fatalf("a direct dependency survives: %+v", c.Removed)
	}
	if fa := h.mod("fabric-api"); len(fa.RequiredBy) != 0 {
		t.Fatalf("fabric-api after removing sodium: %+v", fa)
	}
}

func TestReconcileFollowsWhatTheManifestLists(t *testing.T) {
	_, _, h := twoHosts(t)
	h.mustAdd("sodium", AddOptions{})
	delete(h.r.Manifest.Requires, "sodium")

	before := h.r.Snapshot()
	if reresolved, err := h.reconcile(); err != nil || len(reresolved) != 0 {
		t.Fatalf("reconcile: %v %v", reresolved, err)
	}
	if c := h.r.Changes(before); len(c.Removed) != 2 || len(h.r.Lock.Mods) != 0 {
		t.Fatalf("a mod dropped from the manifest by hand leaves with its dependency: %+v %v", c.Removed, h.r.Lock.Mods)
	}

	h.r.Manifest.Requires["sodium"] = manifest.Require{}
	before = h.r.Snapshot()
	h.mustReconcile()
	if c := h.r.Changes(before); len(c.Added) != 2 || c.Added[0].ID != "fabric-api" || c.Added[1].ID != "sodium" {
		t.Fatalf("a mod listed by hand is locked with its dependency: %+v", c.Added)
	}
	if fa := h.mod("fabric-api"); len(fa.RequiredBy) != 1 || fa.RequiredBy[0] != "sodium" {
		t.Fatalf("fabric-api: %+v", fa)
	}
}
