package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/config"
)

func registryPath(h *harness) string {
	return filepath.Join(filepath.Dir(h.config), config.RegistryFileName)
}

func readLinks(t *testing.T, h *harness) []config.Link {
	t.Helper()
	links, err := config.LoadLinks(registryPath(h))
	if err != nil {
		t.Fatal(err)
	}
	return links
}

func TestSyncIntoRegisters(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	h.mustRun(t, "sync", h.dir)
	h.mustRun(t, "sync", h.dir, "--into", filepath.Join(h.dir, "build", "client"))
	if links := readLinks(t, h); len(links) != 0 {
		t.Fatalf("syncing into the build directory must not register: %+v", links)
	}
	code, stdout, _ := h.run(t, "sync", h.dir, "--name", "Mine", "--json")
	if code == 0 || failureCode(t, stdout).Code != "usage" {
		t.Fatalf("--name without --into: exit %d %s", code, stdout)
	}

	into := filepath.Join(t.TempDir(), "instance")
	stdout = h.mustRun(t, "sync", h.dir, "--into", into)
	links := readLinks(t, h)
	want := config.Link{Side: "client", Name: links[0].Name, Dir: into, Source: h.dir, Target: "client"}
	if len(links) != 1 || links[0] != want || want.Name == "" {
		t.Fatalf("entry: %+v", links)
	}
	if !strings.Contains(stdout, `Registered "`+want.Name+`" (client).`) {
		t.Fatalf("sync should say it registered the directory: %s", stdout)
	}
	if stdout := h.mustRun(t, "sync", h.dir, "--into", into); strings.Contains(stdout, "Registered") {
		t.Fatalf("an unchanged entry is not registered again: %s", stdout)
	}

	if stdout := h.mustRun(t, "sync", h.dir, "--into", into, "--name", "Mine"); !strings.Contains(stdout, `Registered "Mine" (client).`) {
		t.Fatalf("--name renames the entry: %s", stdout)
	}
	h.mustRun(t, "sync", h.dir, "--into", into)
	if links := readLinks(t, h); len(links) != 1 || links[0].Name != "Mine" {
		t.Fatalf("a later sync keeps the name: %+v", links)
	}
}

func TestLinkRegisters(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	prismDir := t.TempDir()
	stdout := h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends")
	if strings.Contains(stdout, "Registered") {
		t.Fatalf("the first sync matches the entry link just wrote: %s", stdout)
	}
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")
	prism := config.Link{Launcher: "prism", LauncherDir: prismDir, Side: "client", Name: "Friends", Dir: gameDir, Source: h.dir, Target: "client"}
	if links := readLinks(t, h); len(links) != 1 || links[0] != prism {
		t.Fatalf("prism entry: %+v", links)
	}
	if stdout := h.mustRun(t, "sync", h.dir, "--target", "client", "--into", gameDir); strings.Contains(stdout, "Registered") {
		t.Fatalf("a pre-launch sync must not change the entry: %s", stdout)
	}

	mojangDir := t.TempDir()
	h.mustRun(t, "link", "mojang", "--launcher-dir", mojangDir)
	links := readLinks(t, h)
	if len(links) != 2 || links[0] != prism {
		t.Fatalf("entries after link mojang: %+v", links)
	}
	if m := links[1]; m.Launcher != "mojang" || m.LauncherDir != mojangDir || m.Dir != filepath.Join(h.dir, "build", "client") || m.Source != h.dir || m.Side != "client" {
		t.Fatalf("mojang entry: %+v", m)
	}

	multimcDir := t.TempDir()
	h.mustRun(t, "link", "multimc", "--launcher-dir", multimcDir)
	if links := readLinks(t, h); len(links) != 3 || links[2].Launcher != "multimc" {
		t.Fatalf("multimc entry: %+v", links)
	}
}

func TestLinksList(t *testing.T) {
	h := newHarness(t)
	if stdout := h.mustRun(t, "links"); !strings.Contains(stdout, "Nothing is linked yet") {
		t.Fatalf("empty registry: %s", stdout)
	}
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir, "--name", "Zed")
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Alpha")
	h.mustRun(t, "link", "mojang", "--launcher-dir", t.TempDir())
	plain := filepath.Join(t.TempDir(), "plain")
	h.mustRun(t, "sync", h.dir, "--into", plain, "--name", "Plain")
	gone := filepath.Join(t.TempDir(), "gone")
	h.mustRun(t, "sync", h.dir, "--into", gone, "--name", "Gone")
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(t.TempDir(), "locked")
	h.mustRun(t, "sync", h.dir, "--into", locked, "--name", "Locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })

	var env struct {
		Data []linkEntry `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "links", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range env.Data {
		got = append(got, e.Launcher+":"+e.Name+":"+e.Status)
	}
	want := "prism:Alpha:synced prism:Zed:not-synced mojang:pack:missing :Gone:missing :Locked:unreadable :Plain:synced"
	if strings.Join(got, " ") != want {
		t.Fatalf("entries:\n got %s\nwant %s", strings.Join(got, " "), want)
	}
	if env.Data[0].SyncedAt == "" || env.Data[1].SyncedAt != "" {
		t.Fatalf("syncedAt comes from the state file: %+v", env.Data[:2])
	}

	stdout := h.mustRun(t, "links")
	for _, part := range []string{
		"Prism Launcher\n  Alpha (client), synced ",
		"  Zed (client), not synced yet\n    " + filepath.Join(prismDir, "instances", "shulker-zed", "minecraft") + "\n    from " + h.dir + ", target client\n",
		"\n\nMinecraft Launcher\n  pack (client), directory is missing\n",
		"\n\nOther directories\n  Gone (client), directory is missing\n",
		"  Locked (client), can't read the directory\n",
	} {
		if !strings.Contains(stdout, part) {
			t.Fatalf("links output lacks %q:\n%s", part, stdout)
		}
	}
}

func TestSyncInstance(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends")
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")
	plain := filepath.Join(t.TempDir(), "plain")
	h.mustRun(t, "sync", h.dir, "--into", plain, "--name", "friends")

	syncDir := func(args ...string) string {
		t.Helper()
		var env struct {
			Data syncResult `json:"data"`
		}
		if err := json.Unmarshal([]byte(h.mustRun(t, append(args, "--json")...)), &env); err != nil {
			t.Fatal(err)
		}
		return env.Data.Dir
	}
	if dir := syncDir("sync", "--instance", "Friends", "--launcher", "prism"); dir != gameDir {
		t.Fatalf("--launcher narrows the name: %s", dir)
	}
	if dir := syncDir("sync", "--instance", plain); dir != plain {
		t.Fatalf("a directory selects its entry: %s", dir)
	}
	if dir := syncDir("sync", "--instance", "FRIENDS", "--side", "client", "--launcher", "prism"); dir != gameDir {
		t.Fatalf("names match case-insensitively: %s", dir)
	}

	for _, c := range []struct {
		args []string
		code string
	}{
		{[]string{"sync", "--instance", "Friends"}, "ambiguous-instance"},
		{[]string{"sync", "--instance", "nope"}, "instance-not-found"},
		{[]string{"sync", "--instance", "Friends", "--side", "server"}, "instance-not-found"},
		{[]string{"sync", "-C", t.TempDir()}, "ambiguous-instance"},
		{[]string{"sync", h.dir, "--instance", "Friends"}, "usage"},
		{[]string{"sync", h.dir, "--launcher", "prism"}, "usage"},
		{[]string{"sync", "--instance", "Friends", "--into", plain}, "usage"},
		{[]string{"sync", "--all", "--launcher", "gdlauncher"}, "usage"},
	} {
		code, stdout, _ := h.run(t, append(c.args, "--json")...)
		if e := failureCode(t, stdout); code == 0 || e.Code != c.code {
			t.Fatalf("%v: exit %d, want %s: %s", c.args, code, c.code, stdout)
		}
		if c.code == "ambiguous-instance" && len(failureCode(t, stdout).Candidates) != 2 {
			t.Fatalf("%v should list both entries: %s", c.args, stdout)
		}
	}

	h.tty, h.stdin = true, strings.NewReader("2\n")
	stdout, stderr := h.mustRunStderr(t, "sync", "-C", t.TempDir())
	if !strings.Contains(stderr, "  2) friends (client)  "+plain) || !strings.Contains(stderr, "Sync which one? [1-2]") || !strings.Contains(stdout, "into "+plain) {
		t.Fatalf("picker:\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	h.tty = false

	stdout = h.mustRun(t, "sync", "--all")
	if !strings.Contains(stdout, "Friends (client, Prism Launcher)\nFetched 0 file(s).\n") || !strings.Contains(stdout, "\n\nfriends (client)\n") {
		t.Fatalf("sync --all output: %s", stdout)
	}
	var all struct {
		Data []syncLinkResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "sync", "--all", "--launcher", "prism", "--json")), &all); err != nil || len(all.Data) != 1 || all.Data[0].Dir != gameDir {
		t.Fatalf("--all returns a list even for one entry: %+v %v", all.Data, err)
	}

	if err := os.RemoveAll(filepath.Dir(gameDir)); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := h.run(t, "sync", "--all", "--json")
	var env struct {
		OK    bool             `json:"ok"`
		Data  []syncLinkResult `json:"data"`
		Error struct{ Code string }
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if code == 0 || env.OK || env.Error.Code != "sync-failed" || len(env.Data) != 2 || env.Data[0].OK || env.Data[0].Error.Code != "instance-missing" || !env.Data[1].OK || env.Data[1].Sync == nil {
		t.Fatalf("a failed entry doesn't stop the others: exit %d %s", code, stdout)
	}
}

func TestSyncInstanceLinkedBySymlink(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "build")
	h.mustRun(t, "link", "prism", "--launcher-dir", t.TempDir(), "--mode", "symlink")
	h.mustRun(t, "sync", "--instance", "pack")
	if links := readLinks(t, h); len(links) != 1 {
		t.Fatalf("syncing through the symlink must not add an entry: %+v", links)
	}
	if data, err := os.ReadFile(filepath.Join(h.dir, "shulker.local.json")); err == nil && strings.Contains(string(data), "syncDirs") {
		t.Fatalf("the build directory reached through a symlink is not a sync dir: %s", data)
	}
}

func TestFeatureInstance(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	setMod(t, h, "sodium", map[string]any{"feature": "fancy"})
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends")
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")
	sodium := filepath.Join(gameDir, "mods", h.jars["sodium"].filename)
	if _, err := os.Stat(sodium); !os.IsNotExist(err) {
		t.Fatalf("sodium is gated off by default: %v", err)
	}

	if stdout := h.mustRun(t, "feature", "on", "fancy", "--instance", "friends", "--launcher", "prism", "--sync"); !strings.Contains(stdout, "fancy on in "+gameDir) {
		t.Fatalf("feature on --instance: %s", stdout)
	}
	if _, err := os.Stat(sodium); err != nil {
		t.Fatalf("--sync should ship the mod: %v", err)
	}
	if stdout := h.mustRun(t, "feature", "list", "--instance", "Friends"); !strings.Contains(stdout, "on (your choice)") {
		t.Fatalf("feature list --instance: %s", stdout)
	}
	for _, args := range [][]string{
		{"feature", "on", "fancy", "--instance", "Friends", "--into", gameDir},
		{"feature", "list", "--launcher", "prism"},
	} {
		code, stdout, _ := h.run(t, append(args, "--json")...)
		if code == 0 || failureCode(t, stdout).Code != "usage" {
			t.Fatalf("%v: exit %d %s", args, code, stdout)
		}
	}
}

func TestSyncWarnsWhenConfigIsUnwritable(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	h.config = filepath.Join(blocker, "config.json")
	stdout, stderr := h.mustRunStderr(t, "sync", h.dir, "--into", filepath.Join(t.TempDir(), "one"))
	if !strings.Contains(stderr, "warning: registry not updated") || strings.Contains(stdout, "Registered") {
		t.Fatalf("stdout: %s\nstderr: %s", stdout, stderr)
	}
}

func TestSyncDetectsAPrismInstance(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends")
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")

	// An entry an older shulker wrote, with no launcher recorded.
	if _, err := config.UpdateLinks(registryPath(h), func(links []config.Link) []config.Link {
		links[0].Launcher, links[0].LauncherDir = "", ""
		return links
	}); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "sync", h.dir, "--target", "client", "--into", gameDir)
	links := readLinks(t, h)
	if len(links) != 1 || links[0].Launcher != "prism" || links[0].LauncherDir != prismDir {
		t.Fatalf("a sync into a Prism instance records the launcher: %+v", links)
	}
	if stdout := h.mustRun(t, "links"); !strings.Contains(stdout, "Prism Launcher") {
		t.Fatalf("links groups it under its launcher: %s", stdout)
	}
}
