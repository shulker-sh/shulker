package launcher

import (
	"os"
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/config"
)

// No command registers a row without a launcher any more: `link` always records one and
// `instances repair` is launcher-only. The fallback stays for a row whose directory matches no
// layout, so it is covered here rather than through the CLI.
func TestAnEntryWithNoLauncherFallsBackToADirectory(t *testing.T) {
	if got := Title(""); got != "Other directories" {
		t.Fatalf("Title = %q", got)
	}
	if Rank("") <= Rank("gdlauncher") {
		t.Fatalf("a plain directory sorts after every launcher: %d", Rank(""))
	}
	l := Linked{Instance: config.Instance{Name: "Server Copy", Dir: "/srv/mc", Source: "/packs/smp"}, Side: "server"}
	command, in := Relink(l)
	if want := "shulker sync /packs/smp --side server --into /srv/mc"; command != want || in != "" {
		t.Fatalf("Relink = %q in %q, want %q", command, in, want)
	}
	f, err := Forget(l.Instance)
	if err != nil || f.Summary != `Forgot "Server Copy" (/srv/mc); its files stay.` {
		t.Fatalf("Forget = %+v %v", f, err)
	}
}

// Shulker is a launcher like the rest, so an instance it owns reaches every command through the
// same entry. What it has none of is slots: it runs the hooks itself, so nothing writes a command
// for another program to run, and forgetting one takes nothing away but the registry row.
func TestShulkerOwnsItsInstances(t *testing.T) {
	e := Find("shulker")
	if e == nil {
		t.Fatal("no shulker entry")
	}
	if !e.IsInstanced || e.InstanceDir("/d/instances/smp") != "/d/instances/smp" {
		t.Fatalf("a shulker instance is its own game directory: %+v", e)
	}
	if e.Slot != nil {
		t.Fatal("shulker fills no slot: it runs the hooks itself")
	}
	if Rank("shulker") >= Rank("prism") {
		t.Fatalf("shulker's own instances sort first: %d", Rank("shulker"))
	}
	if Title("shulker") != "Shulker" {
		t.Fatalf("Title = %q", Title("shulker"))
	}

	in := config.Instance{ID: "smp", Name: "SMP", Launcher: "shulker", Dir: "/d/instances/smp", Source: "/packs/smp"}
	command, at := Relink(Linked{Instance: in, Side: "client", Ref: "main"})
	if want := "shulker link shulker /packs/smp --ref main --as smp"; command != want || at != "" {
		t.Fatalf("Relink = %q in %q, want %q", command, at, want)
	}
	f, err := Forget(in)
	if err != nil || f.Removed != "" || f.Summary != `Unlinked "SMP" (Shulker); the instance directory and its worlds stay.` {
		t.Fatalf("Forget = %+v %v", f, err)
	}
}

func TestRefreshRowKeepsTheOldRowsIdentityAndDetectsAMissingLauncher(t *testing.T) {
	root := t.TempDir()
	gameDir := filepath.Join(root, "instances", "smp", ".minecraft")
	if err := os.MkdirAll(gameDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{PrismPackFile: "{}", PrismInstanceFile: "[General]\nConfigVersion=1.2\n"} {
		if err := os.WriteFile(filepath.Join(root, "instances", "smp", name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := config.Instance{ID: "smp", Name: "SMP", Dir: gameDir, Source: "old", LastSync: "then", LastError: "boom"}
	got := RefreshRow(old, config.Instance{Dir: gameDir, Source: "new"})
	if got.ID != "smp" || got.Name != "SMP" || got.Source != "new" || got.LastSync != "then" || got.LastError != "boom" {
		t.Fatalf("row = %+v", got)
	}
	if got.Launcher != "prism" || got.LauncherDir != root {
		t.Fatalf("a row without a launcher gets the detected one: %+v", got)
	}
	kept := RefreshRow(config.Instance{ID: "smp", Launcher: "atlauncher", LauncherDir: "/atl"}, config.Instance{Dir: gameDir})
	if kept.Launcher != "atlauncher" || kept.LauncherDir != "/atl" {
		t.Fatalf("a recorded launcher is kept: %+v", kept)
	}
}

func TestOwnedIsShulkersInstancesOfAProjectButNotTheProjectItself(t *testing.T) {
	registry := []config.Instance{
		{ID: "smp", Launcher: "shulker", Dir: "/data/instances/smp", Source: "/packs/smp"},
		{ID: "prism", Launcher: "prism", Dir: "/prism/smp/.minecraft", Source: "/packs/smp"},
		{ID: "self", Launcher: "shulker", Dir: "/packs/smp", Source: "/packs/smp"},
		{ID: "other", Launcher: "shulker", Dir: "/data/instances/other", Source: "/packs/other"},
	}
	own := Owned(registry, "/packs/smp/")
	if len(own) != 1 || own[0].ID != "smp" {
		t.Fatalf("owned = %+v", own)
	}
}
