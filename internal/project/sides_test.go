package project

import (
	"slices"
	"testing"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

func serverOnly() *manifest.Manifest {
	return &manifest.Manifest{Server: &manifest.Server{}}
}

func TestSyncSideAssumesAClientOnlyWhenNoneIsDeclared(t *testing.T) {
	side, assumed, err := SyncSide(serverOnly(), "", true)
	if err != nil || side != "client" || !assumed {
		t.Fatalf("got %q assumed=%v err=%v, want an assumed client", side, assumed, err)
	}
	both := &manifest.Manifest{Client: &manifest.Client{}, Server: &manifest.Server{}}
	if side, assumed, err := SyncSide(both, "client", true); err != nil || side != "client" || assumed {
		t.Fatalf("a declared client is never assumed: %q assumed=%v err=%v", side, assumed, err)
	}
	if _, _, err := SyncSide(serverOnly(), "server", true); out.CodeOf(err) != "usage" {
		t.Fatalf("--assume-client with --side server is a usage error, got %v", err)
	}
	if _, _, err := SyncSide(serverOnly(), "client", false); out.CodeOf(err) != "no-side" {
		t.Fatalf("a client the manifest lacks is no-side, got %v", err)
	}
}

func TestExportSidesPutsTheAssumedClientFirst(t *testing.T) {
	sides, assumed, err := ExportSides(serverOnly(), "", true)
	if err != nil || !assumed || !slices.Equal(sides, []string{"client", "server"}) {
		t.Fatalf("got %q assumed=%v err=%v", sides, assumed, err)
	}
	if sides, _, err := ExportSides(serverOnly(), "server", false); err != nil || !slices.Equal(sides, []string{"server"}) {
		t.Fatalf("--side narrows to the one named: %q %v", sides, err)
	}
	if _, _, err := ExportSides(serverOnly(), "client", false); out.CodeOf(err) != "no-side" {
		t.Fatalf("an unassumed client is no-side, got %v", err)
	}
	if _, _, err := ExportSides(serverOnly(), "both", false); out.CodeOf(err) != "usage" {
		t.Fatalf("a name that is no side is a usage error, got %v", err)
	}
}

func TestSingleSideNeedsAChoiceBetweenTwo(t *testing.T) {
	both := &manifest.Manifest{Client: &manifest.Client{}, Server: &manifest.Server{}}
	if _, err := SingleSide(both, ""); out.CodeOf(err) != "ambiguous-side" {
		t.Fatalf("got %v", err)
	}
	if side, err := SingleSide(serverOnly(), ""); err != nil || side != "server" {
		t.Fatalf("the only side is taken: %q %v", side, err)
	}
	if sides, err := Sides(both, ""); err != nil || len(sides) != 2 {
		t.Fatalf("Sides with no name is every side: %q %v", sides, err)
	}
}
