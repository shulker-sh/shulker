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
	l := Linked{Instance: config.Instance{ID: "server-copy", Name: "Server Copy", Dir: "/srv/mc", Source: "/packs/smp"}, Side: "server"}
	command, in := Relink(l)
	if want := "shulker sync /packs/smp --side server --into /srv/mc"; command != want || in != "" {
		t.Fatalf("Relink = %q in %q, want %q", command, in, want)
	}
	f, err := Forget(l.Instance)
	if err != nil || f.Summary != "Forgot server-copy (Server Copy); its files stay in /srv/mc." {
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
	if err != nil || f.Removed != "" || f.Summary != "Unlinked smp (Shulker)" || len(f.Details) != 1 || f.Details[0] != "Kept the instance folder and its worlds" {
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

func TestDetectKnowsEveryLaunchersLayout(t *testing.T) {
	write := func(path, data string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	root := t.TempDir()
	dirs := map[string]string{}
	for _, name := range []string{"prism", "multimc", "portable", "atlauncher", "gdlauncher", "mojang"} {
		dirs[name] = filepath.Join(root, name)
	}

	write(filepath.Join(dirs["prism"], "prismlauncher.cfg"), "")
	write(filepath.Join(dirs["prism"], "instances", "smp", PrismPackFile), "{}")
	write(filepath.Join(dirs["prism"], "instances", "smp", PrismInstanceFile), "[General]\n")
	write(filepath.Join(dirs["multimc"], "multimc.cfg"), "")
	write(filepath.Join(dirs["multimc"], "instances", "smp", MultiMCPackFile), "{}")
	write(filepath.Join(dirs["multimc"], "instances", "smp", MultiMCInstanceFile), "ConfigVersion=1.2\n")
	write(filepath.Join(dirs["portable"], "elsewhere", "smp", MultiMCPackFile), "{}")
	write(filepath.Join(dirs["portable"], "elsewhere", "smp", MultiMCInstanceFile), "name=SMP\n")
	write(filepath.Join(dirs["atlauncher"], "instances", "SMP", ATLauncherInstanceFile), "{}")
	write(filepath.Join(dirs["gdlauncher"], "instances", "SMP", GDLauncherInstanceFile), "{}")
	mojangGame := filepath.Join(dirs["mojang"], "shulker", "smp")
	strangerGame := filepath.Join(dirs["mojang"], "shulker", "stranger")
	if err := os.MkdirAll(strangerGame, 0o755); err != nil {
		t.Fatal(err)
	}
	m := &Mojang{Dir: dirs["mojang"]}
	if err := m.WriteProfile(Profile{Key: "shulker-smp", Name: "SMP", VersionID: "26.2", GameDir: mojangGame}); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct{ gameDir, name, dir string }{
		{filepath.Join(dirs["prism"], "instances", "smp", "minecraft"), "prism", dirs["prism"]},
		{filepath.Join(dirs["multimc"], "instances", "smp", ".minecraft"), "multimc", dirs["multimc"]},
		{filepath.Join(dirs["portable"], "elsewhere", "smp", "minecraft"), "multimc", ""},
		{filepath.Join(dirs["atlauncher"], "instances", "SMP"), "atlauncher", dirs["atlauncher"]},
		{filepath.Join(dirs["gdlauncher"], "instances", "SMP", GDLauncherGameDir), "gdlauncher", dirs["gdlauncher"]},
		{mojangGame, "mojang", dirs["mojang"]},
		{strangerGame, "", ""},
		{root, "", ""},
	} {
		if name, dir := Detect(tc.gameDir); name != tc.name || dir != tc.dir {
			t.Errorf("Detect(%s) = %q %q, want %q %q", tc.gameDir, name, dir, tc.name, tc.dir)
		}
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

func TestLaunchesIsWhetherTheRowNamesThisLauncher(t *testing.T) {
	if !Shulker.Launches(config.Instance{Launcher: "shulker"}) {
		t.Fatal("shulker launches its own rows")
	}
	if Shulker.Launches(config.Instance{Launcher: "prism"}) || Shulker.Launches(config.Instance{}) {
		t.Fatal("another launcher's row, or an unlinked one, is not shulker's to launch")
	}
	if !prismEntry.Launches(config.Instance{Launcher: "prism"}) {
		t.Fatal("prism launches its rows")
	}
}

func TestNamedPutsTheIDFirstAndALauncherNameThatDiffersAside(t *testing.T) {
	for _, tc := range []struct {
		in   config.Instance
		want string
	}{
		{config.Instance{ID: "gt-prism", Name: "My SMP", Launcher: "prism"}, "gt-prism (Prism Launcher: My SMP)"},
		{config.Instance{ID: "friends", Name: "Friends", Launcher: "prism"}, "friends (Prism Launcher)"},
		{config.Instance{ID: "copy", Launcher: "prism"}, "copy (Prism Launcher)"},
		{config.Instance{ID: "server-copy", Name: "Server Copy"}, "server-copy (Server Copy)"},
		{config.Instance{ID: "plain"}, "plain"},
	} {
		if got := Named(tc.in); got != tc.want {
			t.Errorf("Named(%+v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
