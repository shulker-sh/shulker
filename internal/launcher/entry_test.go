package launcher

import (
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
