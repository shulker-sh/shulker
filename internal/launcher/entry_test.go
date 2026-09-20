package launcher

import "testing"

import "shulker.sh/shulker/internal/config"

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
