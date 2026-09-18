package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
)

func registryPath(h *harness) string {
	return filepath.Join(filepath.Dir(h.config), config.RegistryFileName)
}

func readInstances(t *testing.T, h *harness) []config.Instance {
	t.Helper()
	instances, err := config.LoadInstances(registryPath(h))
	if err != nil {
		t.Fatal(err)
	}
	return instances
}

func readIntent(t *testing.T, dir string) *instance.File {
	t.Helper()
	f, err := instance.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestSyncIntoRegisters(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	h.mustRun(t, "sync", h.dir)
	h.mustRun(t, "sync", h.dir, "--into", filepath.Join(h.dir, "build", "client"))
	if instances := readInstances(t, h); len(instances) != 0 {
		t.Fatalf("syncing into the build directory must not register: %+v", instances)
	}
	code, stdout, _ := h.run(t, "sync", h.dir, "--name", "Mine", "--json")
	if code == 0 || failureCode(t, stdout).Code != "usage" {
		t.Fatalf("--name without --into: exit %d %s", code, stdout)
	}

	into := filepath.Join(t.TempDir(), "instance")
	stdout = h.mustRun(t, "sync", h.dir, "--into", into)
	instances := readInstances(t, h)
	want := config.Instance{ID: "pack", Name: "pack", Dir: into, Source: h.dir}
	if len(instances) != 1 || instances[0] != want {
		t.Fatalf("instance: %+v", instances)
	}
	if !strings.Contains(stdout, "registered pack") {
		t.Fatalf("sync should say it registered the directory: %s", stdout)
	}
	f := readIntent(t, into)
	if f.Source != h.dir || f.Target != "client" || f.Resolved == nil || f.Resolved.Side != "client" {
		t.Fatalf("instance file: %+v", f)
	}
	if !f.Settings.PreLaunch() || !f.Settings.PostExit() || !f.Settings.MarkerOn() {
		t.Fatalf("settings default on: %+v", f.Settings)
	}
	if stdout := h.mustRun(t, "sync", h.dir, "--into", into); strings.Contains(stdout, "Registered") {
		t.Fatalf("an unchanged instance is not registered again: %s", stdout)
	}

	// The id is the key, so it stays put when the launcher-visible name changes.
	if stdout := h.mustRun(t, "sync", h.dir, "--into", into, "--name", "Mine"); !strings.Contains(stdout, "registered Mine (pack)") {
		t.Fatalf("--name renames the instance: %s", stdout)
	}
	h.mustRun(t, "sync", h.dir, "--into", into)
	if instances := readInstances(t, h); len(instances) != 1 || instances[0].Name != "Mine" || instances[0].ID != "pack" {
		t.Fatalf("a later sync keeps the name and id: %+v", instances)
	}
}

func TestSyncIntoTakesAnID(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	first := filepath.Join(t.TempDir(), "one")
	h.mustRun(t, "sync", h.dir, "--into", first, "--as", "cozy")
	if instances := readInstances(t, h); len(instances) != 1 || instances[0].ID != "cozy" {
		t.Fatalf("--as sets the id: %+v", instances)
	}

	second := filepath.Join(t.TempDir(), "two")
	code, stdout, _ := h.run(t, "sync", h.dir, "--into", second, "--as", "cozy", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "instance-id-taken" || !strings.Contains(e.Message, first) {
		t.Fatalf("a taken id names its holder: exit %d %s", code, stdout)
	}
	code, stdout, _ = h.run(t, "sync", h.dir, "--into", second, "--as", "Not An ID", "--json")
	if code == 0 || failureCode(t, stdout).Code != "usage" {
		t.Fatalf("an invalid id is a usage error: exit %d %s", code, stdout)
	}

	// Two instances whose names slug the same still get ids of their own.
	h.mustRun(t, "sync", h.dir, "--into", second, "--name", "pack")
	third := filepath.Join(t.TempDir(), "three")
	h.mustRun(t, "sync", h.dir, "--into", third, "--name", "pack")
	instances := readInstances(t, h)
	if len(instances) != 3 || instances[1].ID != "pack" || instances[2].ID != "pack-2" {
		t.Fatalf("ids are unique: %+v", instances)
	}
}

func TestLinkRegisters(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	prismDir := t.TempDir()
	stdout := h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends")
	if strings.Contains(stdout, "Registered") {
		t.Fatalf("the first sync matches the instance link just wrote: %s", stdout)
	}
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")
	prism := config.Instance{ID: "friends", Launcher: "prism", LauncherDir: prismDir, Name: "Friends", Dir: gameDir, Source: h.dir}
	if instances := readInstances(t, h); len(instances) != 1 || instances[0] != prism {
		t.Fatalf("prism instance: %+v", instances)
	}
	if f := readIntent(t, gameDir); f.Source != h.dir || f.Target != "client" || f.Resolved.Side != "client" {
		t.Fatalf("instance file: %+v", f)
	}
	if stdout := h.mustRun(t, "sync", h.dir, "--target", "client", "--into", gameDir); strings.Contains(stdout, "Registered") {
		t.Fatalf("a pre-launch sync must not change the instance: %s", stdout)
	}

	mojangDir := t.TempDir()
	h.mustRun(t, "link", "mojang", "--launcher-dir", mojangDir)
	instances := readInstances(t, h)
	if len(instances) != 2 || instances[0] != prism {
		t.Fatalf("instances after link mojang: %+v", instances)
	}
	if m := instances[1]; m.Launcher != "mojang" || m.LauncherDir != mojangDir || m.Dir != filepath.Join(h.dir, "build", "client") || m.Source != h.dir || m.ID != "pack" {
		t.Fatalf("mojang instance: %+v", m)
	}

	multimcDir := t.TempDir()
	h.mustRun(t, "link", "multimc", "--launcher-dir", multimcDir, "--as", "mmc")
	if instances := readInstances(t, h); len(instances) != 3 || instances[2].Launcher != "multimc" || instances[2].ID != "mmc" {
		t.Fatalf("multimc instance: %+v", instances)
	}
}

func TestInstancesList(t *testing.T) {
	h := newHarness(t)
	if stdout := h.mustRun(t, "instances"); !strings.Contains(stdout, "Nothing is linked yet") {
		t.Fatalf("empty registry: %s", stdout)
	}
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
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
		Data []instanceEntry `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "instances", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range env.Data {
		got = append(got, e.Launcher+":"+e.ID+":"+e.Status)
	}
	want := "prism:alpha:synced prism:zed:not-synced mojang:pack:not-synced :gone:missing :locked:unreadable :plain:synced"
	if strings.Join(got, " ") != want {
		t.Fatalf("instances:\n got %s\nwant %s", strings.Join(got, " "), want)
	}
	if env.Data[0].SyncedAt == "" || env.Data[1].SyncedAt != "" {
		t.Fatalf("syncedAt comes from the state file: %+v", env.Data[:2])
	}

	stdout := h.mustRun(t, "instances")
	for _, part := range []string{
		"Prism Launcher\n    ├─ • alpha client (synced ",
		"• zed client (not synced yet)\n         " + filepath.Join(prismDir, "instances", "shulker-zed", "minecraft") + "\n         Zed, from " + h.dir + ", side client\n",
		"\n\n  Minecraft Launcher\n    └─ • pack client (not synced yet)\n",
		"\n\n  Other directories\n    ├─ • gone (directory is missing)\n",
		"• locked (can't read the directory)\n",
	} {
		if !strings.Contains(stdout, part) {
			t.Fatalf("instances output lacks %q:\n%s", part, stdout)
		}
	}
}

func TestSyncInstance(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
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
	// An id is unique, so it needs no narrowing where the shared name does.
	if dir := syncDir("sync", "-i", "friends"); dir != gameDir {
		t.Fatalf("an id picks one instance: %s", dir)
	}
	if dir := syncDir("sync", "--id", "friends-2"); dir != plain {
		t.Fatalf("--id is the same flag: %s", dir)
	}
	if dir := syncDir("sync", "-i", "Friends", "--launcher", "prism"); dir != gameDir {
		t.Fatalf("--launcher narrows the name: %s", dir)
	}
	if dir := syncDir("sync", "-i", plain); dir != plain {
		t.Fatalf("a directory selects its instance: %s", dir)
	}
	if dir := syncDir("sync", "-i", "FRIENDS", "--side", "client", "--launcher", "prism"); dir != gameDir {
		t.Fatalf("names match case-insensitively: %s", dir)
	}

	for _, c := range []struct {
		args []string
		code string
	}{
		{[]string{"sync", "-i", "Friends"}, "ambiguous-instance"},
		{[]string{"sync", "-i", "nope"}, "instance-not-found"},
		{[]string{"sync", "-i", "Friends", "--side", "server"}, "instance-not-found"},
		{[]string{"sync", "-C", t.TempDir()}, "ambiguous-instance"},
		{[]string{"sync", h.dir, "-i", "Friends"}, "usage"},
		{[]string{"sync", h.dir, "--launcher", "prism"}, "usage"},
		{[]string{"sync", "-i", "Friends", "--into", plain}, "usage"},
		{[]string{"sync", "--all", "--launcher", "technic"}, "usage"},
		{[]string{"list", "-i", "friends", "-C", h.dir}, "usage"},
	} {
		code, stdout, _ := h.run(t, append(c.args, "--json")...)
		if e := failureCode(t, stdout); code == 0 || e.Code != c.code {
			t.Fatalf("%v: exit %d, want %s: %s", c.args, code, c.code, stdout)
		}
		if c.code == "ambiguous-instance" && len(failureCode(t, stdout).Candidates) != 2 {
			t.Fatalf("%v should list both instances: %s", c.args, stdout)
		}
	}

	h.tty, h.stdin = true, strings.NewReader("2\n")
	stdout, stderr := h.mustRunStderr(t, "sync", "-C", t.TempDir())
	if !strings.Contains(stderr, " 2) friends client\n     "+plain) || !strings.Contains(stderr, "Sync which one? [1-2]") || !strings.Contains(stdout, "» "+plain) {
		t.Fatalf("picker:\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	h.tty = false

	stdout = h.mustRun(t, "sync", "--all")
	if !strings.Contains(stdout, "  Friends client (Prism Launcher)\n  ✔ synced client") || !strings.Contains(stdout, "\n\n  friends client\n") {
		t.Fatalf("sync --all output: %s", stdout)
	}
	var all struct {
		Data []syncInstanceResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "sync", "--all", "--launcher", "prism", "--json")), &all); err != nil || len(all.Data) != 1 || all.Data[0].Dir != gameDir {
		t.Fatalf("--all returns a list even for one instance: %+v %v", all.Data, err)
	}

	if err := os.RemoveAll(filepath.Dir(gameDir)); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := h.run(t, "sync", "--all", "--json")
	var env struct {
		OK    bool                 `json:"ok"`
		Data  []syncInstanceResult `json:"data"`
		Error struct{ Code string }
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if code == 0 || env.OK || env.Error.Code != "sync-failed" || len(env.Data) != 2 || env.Data[0].OK || env.Data[0].Error.Code != "instance-missing" || !env.Data[1].OK || env.Data[1].Sync == nil {
		t.Fatalf("a failed instance doesn't stop the others: exit %d %s", code, stdout)
	}
}

func TestSyncInstanceLinkedBySymlink(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "build")
	h.mustRun(t, "link", "prism", "--launcher-dir", t.TempDir(), "--mode", "symlink")
	h.mustRun(t, "sync", "-i", "pack")
	if instances := readInstances(t, h); len(instances) != 1 {
		t.Fatalf("syncing through the symlink must not add an instance: %+v", instances)
	}
	if data, err := os.ReadFile(filepath.Join(h.dir, "shulker.local.json")); err == nil && strings.Contains(string(data), "syncDirs") {
		t.Fatalf("the build directory reached through a symlink is not a sync dir: %s", data)
	}
}

func TestFeatureInstance(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	setMod(t, h, "sodium", map[string]any{"feature": "fancy"})
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends")
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")
	sodium := filepath.Join(gameDir, "mods", h.jars["sodium"].filename)
	if _, err := os.Stat(sodium); !os.IsNotExist(err) {
		t.Fatalf("sodium is gated off by default: %v", err)
	}

	if stdout := h.mustRun(t, "feature", "on", "fancy", "-i", "friends", "--launcher", "prism", "--sync"); !strings.Contains(stdout, "fancy on » "+gameDir) {
		t.Fatalf("feature on -i: %s", stdout)
	}
	if _, err := os.Stat(sodium); err != nil {
		t.Fatalf("--sync should ship the mod: %v", err)
	}
	if stdout := h.mustRun(t, "feature", "list", "-i", "Friends"); !strings.Contains(stdout, "fancy on (your choice") {
		t.Fatalf("feature list -i: %s", stdout)
	}
	for _, args := range [][]string{
		{"feature", "on", "fancy", "-i", "Friends", "--into", gameDir},
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
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	h.config = filepath.Join(blocker, "config.json")
	stdout, stderr := h.mustRunStderr(t, "sync", h.dir, "--into", filepath.Join(t.TempDir(), "one"))
	if !strings.Contains(stderr, "! registry not updated") || strings.Contains(stdout, "registered") {
		t.Fatalf("stdout: %s\nstderr: %s", stdout, stderr)
	}
}

func TestSyncDetectsAPrismInstance(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends")
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")

	// A row an older shulker wrote, with no launcher recorded.
	if _, err := config.UpdateInstances(registryPath(h), func(instances []config.Instance) []config.Instance {
		instances[0].Launcher, instances[0].LauncherDir = "", ""
		return instances
	}); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "sync", h.dir, "--target", "client", "--into", gameDir)
	instances := readInstances(t, h)
	if len(instances) != 1 || instances[0].Launcher != "prism" || instances[0].LauncherDir != prismDir {
		t.Fatalf("a sync into a Prism instance records the launcher: %+v", instances)
	}
	if stdout := h.mustRun(t, "instances"); !strings.Contains(stdout, "Prism Launcher") {
		t.Fatalf("instances groups it under its launcher: %s", stdout)
	}
}

func TestInstancesRepair(t *testing.T) {
	h := newHarness(t)
	prismDir := t.TempDir()
	if stdout := h.mustRun(t, "instances", "repair", "--launcher", "prism", "--launcher-dir", prismDir); !strings.Contains(stdout, "Nothing is linked yet") {
		t.Fatalf("repair with nothing to find: %s", stdout)
	}
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends")
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")

	// A registry shulker can't read is rebuilt from what the launcher holds, and
	// an instance missing its file gets one from what its build recorded.
	if err := os.WriteFile(registryPath(h), []byte("{ not a registry"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(instance.Path(gameDir)); err != nil {
		t.Fatal(err)
	}
	stdout, stderr := h.mustRunStderr(t, "instances", "repair", "--launcher", "prism", "--launcher-dir", prismDir)
	if !strings.Contains(stderr, "rebuilding it") || !strings.Contains(stdout, "registered ") {
		t.Fatalf("repair:\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	instances := readInstances(t, h)
	if len(instances) != 1 || instances[0].Dir != gameDir || instances[0].Source != h.dir || instances[0].Launcher != "prism" {
		t.Fatalf("the scan registers the instance again: %+v", instances)
	}
	if f := readIntent(t, gameDir); f.Source != h.dir || f.Target != "client" {
		t.Fatalf("repair writes the instance file: %+v", f)
	}

	// A directory that is gone is reported, never dropped: unlink is what forgets.
	if err := os.RemoveAll(filepath.Dir(gameDir)); err != nil {
		t.Fatal(err)
	}
	if stdout := h.mustRun(t, "instances", "repair", "--launcher", "prism", "--launcher-dir", prismDir); !strings.Contains(stdout, "missing") {
		t.Fatalf("a missing directory is reported: %s", stdout)
	}
	if instances := readInstances(t, h); len(instances) != 1 {
		t.Fatalf("a missing directory keeps its row: %+v", instances)
	}
}
