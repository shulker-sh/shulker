package resolve

import (
	"archive/zip"
	"bytes"
	"context"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/env/envtest"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/provider"
)

func zipOf(t *testing.T, names ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, name := range names {
		if _, err := zw.Create(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// serverPackHarness is a project whose three mods an import locked, and a host with the pack
// and the server files it pairs with, which list server.
func serverPackHarness(t *testing.T, server []byte, unhashed ...string) (*harness, []byte) {
	t.Helper()
	c := envtest.NewCDN(t)
	alpha := envtest.NewHost(c, "alpha")
	alpha.UnhashedTypes = unhashed
	pack := zipOf(t, "manifest.json")
	atm := provider.Project{ID: "p-atm", Slug: "atm", Title: "All the Mods", Type: manifest.TypeModpack}
	files := alpha.Publish(atm, provider.Version{ID: "server-1", Number: "8.2 server"}, server)
	alpha.Publish(atm, provider.Version{Number: "8.2", ServerPack: files.ID}, pack)
	h := newHarness(t, alpha)
	for id, m := range map[string]lock.Mod{
		"sodium": {Filename: "sodium.jar", Side: "both"},
		"jei":    {Filename: "jei.jar", Side: "both"},
		"iris":   {Filename: "iris.jar", Side: "client", SideFrom: "dependencies"},
		"lith":   {Filename: "lith.jar", Side: "server", SideFrom: "requires"},
		"zoom":   {Filename: "zoom.jar", Side: "client", SideFrom: "dependencies"},
	} {
		h.r.Lock.Mods[id] = m
		h.r.Manifest.Requires[id] = manifest.Require{}
	}
	h.r.Manifest.Requires["lith"] = manifest.Require{Side: "server"}
	return h, pack
}

func TestAServerPackDecidesTheSidesOfTheModsItListsOrLeavesOut(t *testing.T) {
	h, pack := serverPackHarness(t, zipOf(t, "Pack/mods/jei.jar", "Pack/mods/iris.jar", "Pack/config/x/mods/sodium.jar"))
	sp, err := h.r.ReadServerPack(context.Background(), "", pack)
	if err != nil {
		t.Fatal(err)
	}
	if sp == nil || !slices.Equal(sp.Client, []string{"sodium", "zoom"}) || !slices.Equal(sp.Both, []string{"iris"}) {
		t.Fatalf("server pack %+v", sp)
	}
	for id, want := range map[string]string{"sodium": "client", "iris": "both", "jei": "", "lith": "server", "zoom": "client"} {
		if got := h.r.Manifest.Requires[id].Side; got != want {
			t.Errorf("%s requires side %q, want %q", id, got, want)
		}
	}
	if m := h.mod("sodium"); m.Side != "client" || m.SideFrom != "requires" {
		t.Errorf("sodium locked %+v", m)
	}
	if m := h.mod("iris"); m.Side != "both" || m.SideFrom != "requires" {
		t.Errorf("iris locked %+v", m)
	}
}

func TestAServerPackWithNoModsFolderChangesNothing(t *testing.T) {
	h, pack := serverPackHarness(t, zipOf(t, "startserver.sh", "config/a.toml"))
	sp, err := h.r.ReadServerPack(context.Background(), "", pack)
	if err == nil || sp != nil {
		t.Fatalf("server pack %+v, err %v", sp, err)
	}
	if h.r.Manifest.Requires["sodium"].Side != "" {
		t.Fatal("sodium was given a side")
	}
}

func TestAPackWithNoServerPackReadsNothing(t *testing.T) {
	h, _ := serverPackHarness(t, zipOf(t, "mods/jei.jar"))
	sp, err := h.r.ReadServerPack(context.Background(), "atm", zipOf(t, "unknown.json"))
	if err != nil || sp != nil {
		t.Fatalf("server pack %+v, err %v", sp, err)
	}
}

func TestAPackOutsideTheHashIndexIsFoundByNameAndSHA1(t *testing.T) {
	h, pack := serverPackHarness(t, zipOf(t, "mods/jei.jar"), manifest.TypeModpack)
	sp, err := h.r.ReadServerPack(context.Background(), "All the Mods", pack)
	if err != nil {
		t.Fatal(err)
	}
	if sp == nil || !slices.Equal(sp.Client, []string{"iris", "sodium", "zoom"}) {
		t.Fatalf("server pack %+v", sp)
	}
}
