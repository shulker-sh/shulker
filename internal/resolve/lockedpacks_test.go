package resolve

import (
	"context"
	"maps"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
)

// lockedPack is a directory modpack listing keys, whose lock is a copy of the project's own, the
// way a modpack resolved on the same providers and platform would carry.
func (h *harness) lockedPack(name string, keys ...string) *modpack.Loaded {
	m := &manifest.Manifest{Name: name, Minecraft: "~26.2", Loader: manifest.Loader{Type: "fabric", Version: "*"}, Requires: map[string]manifest.Require{}, Client: &manifest.Client{}}
	for _, key := range keys {
		m.Requires[key] = h.r.Manifest.Requires[key]
	}
	l := lock.New()
	l.Minecraft, l.Loader = h.r.Lock.Minecraft, h.r.Lock.Loader
	maps.Copy(l.Mods, h.r.Lock.Mods)
	for _, kind := range manifest.PackKinds {
		maps.Copy(l.Packs(kind), h.r.Lock.Packs(kind))
	}
	return &modpack.Loaded{Name: name, Source: "./" + name, Kind: modpack.Local, Manifest: m, Lock: l, UsesLock: true, Pin: lock.Modpack{Source: "./" + name}}
}

// followLocked hands the named keys to a locked modpack and drops them from the project itself.
func (h *harness) followLocked(name string, keys ...string) {
	h.t.Helper()
	p := h.lockedPack(name, keys...)
	if err := h.r.Remove(keys); err != nil {
		h.t.Fatal(err)
	}
	h.r.Manifest.Requires[name] = manifest.Require{Source: p.Source}
	if err := h.r.AddPack(context.Background(), p); err != nil {
		h.t.Fatal(err)
	}
}

func TestUpdateRefusesALockedModpacksMod(t *testing.T) {
	_, _, h := twoHosts(t)
	h.mustAdd("sodium", AddOptions{})
	h.followLocked("base", "sodium")

	if got := h.mod("sodium").Modpack; got != "base" {
		t.Fatalf("sodium should come from the modpack, got %q", got)
	}
	err := h.r.Update(context.Background(), []string{"sodium"})
	if e := out.AsError(err); e == nil || e.Code != "modpack-provided" {
		t.Fatalf("a mod a locked modpack pins is not updatable here: %v", err)
	}
}

func TestRemovingALockedModpackDropsItsResourcePacks(t *testing.T) {
	cf := curseForgeHost(t)
	h := newHarness(t, cf)
	h.mustAdd("sodium", AddOptions{})
	h.mustAdd("fresh-animations", AddOptions{})
	h.followLocked("base", "sodium", "fresh-animations")

	if got := h.r.Lock.ResourcePacks["fresh-animations"].Modpack; got != "base" {
		t.Fatalf("fresh-animations should come from the modpack, got %q", got)
	}
	if err := h.r.RemovePack("base"); err != nil {
		t.Fatal(err)
	}
	if got, still := h.r.Lock.ResourcePacks["fresh-animations"]; still {
		t.Fatalf("the modpack's resource pack should leave with it: %+v", got)
	}
	if len(h.r.Lock.Mods) != 0 {
		t.Fatalf("the modpack's mods should leave with it: %v", h.r.Lock.Mods)
	}
}
