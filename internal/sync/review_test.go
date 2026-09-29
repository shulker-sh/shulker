package sync

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/env/envtest"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/provider"
)

// reviewHarness is a harness synced once into a directory, with lithium published and ready to
// add, so the next sync brings a change.
func reviewHarness(t *testing.T) (*harness, string, provider.Version) {
	h := newHarness(t)
	h.add("sodium")
	into := filepath.Join(t.TempDir(), "instance")
	h.mustSync(into, Request{})
	lithium := h.env.Modrinth.Publish(provider.Project{ID: "gvQqBUqZ", Slug: "lithium", Title: "Lithium"}, provider.Version{ID: "LiThIuM1", Number: "0.14", File: provider.File{Filename: "lithium-0.14.jar"}}, envtest.ModJar(t, "lithium", "0.14", "client"))
	h.add("lithium")
	return h, into, lithium
}

func TestAFirstSyncHasNothingToReview(t *testing.T) {
	h := newHarness(t)
	h.add("sodium")
	asked := false

	res := h.mustSync(filepath.Join(t.TempDir(), "instance"), Request{Review: func(string, *instance.Changes) (bool, error) {
		asked = true
		return true, nil
	}})

	if asked || res.Review != nil {
		t.Fatalf("a first sync asks nothing: %+v", res.Review)
	}
}

func TestSyncWithNobodyToAskAppliesItsChangesAndWarns(t *testing.T) {
	h, into, lithium := reviewHarness(t)

	res := h.mustSync(into, Request{})

	if res.Review == nil || len(res.Review.Added) != 1 || res.Review.Added[0].Key != "lithium" {
		t.Fatalf("review: %+v", res.Review)
	}
	if !exists(filepath.Join(into, "mods", lithium.File.Filename)) {
		t.Fatal("the change is applied")
	}
	var warned bool
	for _, w := range h.env.Security {
		warned = warned || w.Protection == "sync-review" && strings.Contains(w.Message, "adds lithium from Modrinth")
	}
	if !warned {
		t.Fatalf("the changes are warned about: %v", h.env.Warnings)
	}
	if st := instance.LoadState(into); len(st.Changelog) != 1 || st.Changelog[0].Added[0].Key != "lithium" {
		t.Fatalf("the changelog records the sync: %+v", st.Changelog)
	}

	h.mustSync(into, Request{})
	if st := instance.LoadState(into); len(st.Changelog) != 1 {
		t.Fatalf("a sync with no changes keeps the changelog as it is: %+v", st.Changelog)
	}
}

func TestSyncDeclinedLeavesTheDirectoryAsItWas(t *testing.T) {
	h, into, lithium := reviewHarness(t)
	before := instance.LoadState(into)
	var shown *instance.Changes

	res := h.mustSync(into, Request{Review: func(dir string, c *instance.Changes) (bool, error) {
		shown = c
		return false, nil
	}})

	if !res.Declined || shown == nil || len(shown.Added) != 1 {
		t.Fatalf("declined %v, shown %+v", res.Declined, shown)
	}
	if exists(filepath.Join(into, "mods", lithium.File.Filename)) {
		t.Fatal("a declined sync places nothing")
	}
	if after := instance.LoadState(into); after.BuiltAt != before.BuiltAt || len(after.Changelog) != 0 {
		t.Fatalf("a declined sync records nothing: %+v", after)
	}
	if h.warned("applied without asking") {
		t.Fatalf("a declined sync warns about nothing applied: %v", h.env.Warnings)
	}

	res = h.mustSync(into, Request{Review: func(string, *instance.Changes) (bool, error) { return true, nil }})
	if res.Declined || !exists(filepath.Join(into, "mods", lithium.File.Filename)) {
		t.Fatal("an accepted review applies the changes")
	}
	if h.warned("applied without asking") {
		t.Fatalf("a sync the player was asked about doesn't warn: %v", h.env.Warnings)
	}
}

func TestDecliningAnInPlaceSyncPutsTheRelockBack(t *testing.T) {
	h := newHarness(t)
	h.editManifest(func(m *manifest.Manifest) { m.Client.Build = "." })
	h.add("sodium")
	h.mustSync(h.dir, Request{})
	lithium := h.env.Modrinth.Publish(provider.Project{ID: "gvQqBUqZ", Slug: "lithium", Title: "Lithium"}, provider.Version{ID: "LiThIuM1", Number: "0.14", File: provider.File{Filename: "lithium-0.14.jar"}}, envtest.ModJar(t, "lithium", "0.14", "client"))
	h.add("lithium")
	h.editLock(func(l *lock.Lock) { delete(l.Mods, "lithium") })
	manifestBefore, lockBefore := readFile(t, filepath.Join(h.dir, manifest.FileName)), readFile(t, filepath.Join(h.dir, lock.FileName))
	historyBefore, err := build.History(h.dir)
	if err != nil {
		t.Fatal(err)
	}

	res, err := InPlace(context.Background(), h.e, h.project(), "client", Request{Reason: "sync", Review: func(string, *instance.Changes) (bool, error) { return false, nil }})

	if err != nil || !res.Declined {
		t.Fatalf("declined %v, err %v", res.Declined, err)
	}
	if readFile(t, filepath.Join(h.dir, lock.FileName)) != lockBefore || readFile(t, filepath.Join(h.dir, manifest.FileName)) != manifestBefore {
		t.Fatal("a declined in-place sync puts the manifest and lock back")
	}
	if history, err := build.History(h.dir); err != nil || len(history) != len(historyBefore) {
		t.Fatalf("a declined sync keeps no history entry: %d before, %d after, %v", len(historyBefore), len(history), err)
	}
	if exists(filepath.Join(h.dir, "mods", lithium.File.Filename)) {
		t.Fatal("a declined sync places nothing")
	}
}
