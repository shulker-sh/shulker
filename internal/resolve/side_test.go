package resolve

import (
	"slices"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/packarchive"
)

func TestKeepSideDropsTheOtherSidesEntriesBlockAndLayers(t *testing.T) {
	m := &manifest.Manifest{
		Server:   &manifest.Server{},
		Requires: map[string]manifest.Require{"sodium": {Side: "client"}, "spark": {Side: "server"}, "both": {}},
	}
	l := lock.New()
	l.Mods["sodium"] = lock.Mod{Side: "client"}
	l.Mods["spark"] = lock.Mod{Side: "server"}
	l.Mods["both"] = lock.Mod{}
	l.Players = []lock.Player{{Name: "steve"}}
	overrides := []packarchive.Override{{Layer: "overrides", Path: "a"}, {Layer: "server-overrides", Path: "b"}, {Layer: "client-overrides", Path: "c"}}
	kept, leftOut := KeepSide(m, l, overrides, "client")
	if want := []string{"server-overrides/b", "spark"}; !slices.Equal(leftOut, want) {
		t.Fatalf("leftOut = %v, want %v", leftOut, want)
	}
	if len(kept) != 2 || kept[0].Path != "a" || kept[1].Path != "c" {
		t.Fatalf("kept = %v", kept)
	}
	if m.Server != nil || m.Client == nil || len(l.Players) != 0 {
		t.Fatalf("server block %v, client block %v, players %v", m.Server, m.Client, l.Players)
	}
	if _, ok := m.Requires["spark"]; ok {
		t.Fatal("spark should have left the manifest")
	}
	if _, ok := l.Mods["spark"]; ok {
		t.Fatal("spark should have left the lock")
	}
}
