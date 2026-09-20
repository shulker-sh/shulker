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

// withoutStamp checks that the sync stamped the row, then takes the stamp off so the rest of the
// row can be compared whole.
func withoutStamp(t *testing.T, in config.Instance) config.Instance {
	t.Helper()
	if in.LastSync == "" || in.LastError != "" {
		t.Fatalf("a sync that worked should stamp the row: %+v", in)
	}
	in.LastSync = ""
	return in
}

func instanceDir(t *testing.T, h *harness, id string) string {
	t.Helper()
	instances := readInstances(t, h)
	for _, in := range instances {
		if in.ID == id {
			return in.Dir
		}
	}
	t.Fatalf("no instance is called %s: %+v", id, instances)
	return ""
}

func readIntent(t *testing.T, dir string) *instance.File {
	t.Helper()
	f, err := instance.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

func TestSyncIntoTakesNoRegistryRow(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	h.mustRun(t, "sync", h.dir)
	h.mustRun(t, "sync", h.dir, "--into", filepath.Join(h.dir, "build", "client"))
	if instances := readInstances(t, h); len(instances) != 0 {
		t.Fatalf("syncing into the build directory registers nothing: %+v", instances)
	}

	into := filepath.Join(t.TempDir(), "instance")
	stdout := h.mustRun(t, "sync", h.dir, "--into", into)
	if instances := readInstances(t, h); len(instances) != 0 {
		t.Fatalf("a detached build takes no registry row: %+v", instances)
	}
	if strings.Contains(strings.ToLower(stdout), "registered") {
		t.Fatalf("sync should report no registration: %s", stdout)
	}
	f := readIntent(t, into)
	if f.Source != h.dir || f.Side != "client" {
		t.Fatalf("instance file: %+v", f)
	}
	if !f.Settings.PreLaunch() || !f.Settings.PostExit() || f.Settings.Marker != nil {
		t.Fatalf("the hooks default on and the marker defers to the manifest: %+v", f.Settings)
	}

	if stdout := h.mustRun(t, "instances"); !strings.Contains(stdout, "Nothing is linked yet") {
		t.Fatalf("a detached build is not an instance: %s", stdout)
	}
	code, stdout, _ := h.run(t, "sync", "-i", "pack", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "no-instances" {
		t.Fatalf("-i must not reach a detached build: exit %d %s", code, stdout)
	}
}

func TestSyncHasNoInstanceIDFlags(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	into := filepath.Join(t.TempDir(), "instance")
	for _, flag := range []string{"--as", "--name"} {
		code, stdout, stderr := h.run(t, "sync", h.dir, "--into", into, flag, "cozy")
		if code == 0 || !strings.Contains(stderr, "unknown flag "+flag) {
			t.Fatalf("%s should be gone from sync: exit %d\nstdout: %s\nstderr: %s", flag, code, stdout, stderr)
		}
	}

	// They stay on link, where an instance still has an id and a name of its own.
	first := t.TempDir()
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", first, "--as", "cozy", "--name", "Friends")
	instances := readInstances(t, h)
	if len(instances) != 1 || instances[0].ID != "cozy" || instances[0].Name != "Friends" {
		t.Fatalf("--as and --name still name a linked instance: %+v", instances)
	}
	second := t.TempDir()
	code, stdout, _ := h.run(t, "link", "prism", h.dir, "--launcher-dir", second, "--as", "cozy", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "instance-id-taken" || !strings.Contains(e.Message, instances[0].Dir) {
		t.Fatalf("a taken id names its holder: exit %d %s", code, stdout)
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
	if instances := readInstances(t, h); len(instances) != 1 || withoutStamp(t, instances[0]) != prism {
		t.Fatalf("prism instance: %+v", instances)
	}
	if f := readIntent(t, gameDir); f.Source != h.dir || f.Side != "client" {
		t.Fatalf("instance file: %+v", f)
	}
	if stdout := h.mustRun(t, "sync", h.dir, "--side", "client", "--into", gameDir); strings.Contains(stdout, "Registered") {
		t.Fatalf("a pre-launch sync must not change the instance: %s", stdout)
	}

	mojangDir := t.TempDir()
	h.mustRun(t, "link", "mojang", "--launcher-dir", mojangDir)
	instances := readInstances(t, h)
	if len(instances) != 2 || withoutStamp(t, instances[0]) != prism {
		t.Fatalf("instances after link mojang: %+v", instances)
	}
	if m := instances[1]; m.Launcher != "mojang" || m.LauncherDir != mojangDir || m.Dir != mojangGameDir(mojangDir, "pack") || m.Source != h.dir || m.ID != "pack" {
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
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Gone")
	if err := os.RemoveAll(instanceDir(t, h, "gone")); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Locked")
	locked := instanceDir(t, h, "locked")
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
	want := "prism:alpha:synced prism:gone:missing prism:locked:unreadable prism:zed:not-synced mojang:pack:synced"
	if strings.Join(got, " ") != want {
		t.Fatalf("instances:\n got %s\nwant %s", strings.Join(got, " "), want)
	}
	if env.Data[0].SyncedAt == "" || env.Data[3].SyncedAt != "" {
		t.Fatalf("syncedAt comes from the state file: %+v", env.Data)
	}

	stdout := h.mustRun(t, "instances")
	for _, part := range []string{
		"Prism Launcher\n    ├─ • alpha client (synced ",
		"• zed client (not synced yet)\n         " + filepath.Join(prismDir, "instances", "shulker-zed", "minecraft") + "\n         Zed, from " + h.dir + ", side client\n",
		"\n\n  Minecraft Launcher\n    └─ • pack client (synced ",
		"• gone (directory is missing)\n",
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
	h.mustRun(t, "link", "multimc", h.dir, "--launcher-dir", t.TempDir(), "--name", "friends")
	plain := instanceDir(t, h, "friends-2")

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
	if !strings.Contains(stderr, " 2) friends client (MultiMC)\n     "+plain) || !strings.Contains(stderr, "Sync which one? [1-2]") || !strings.Contains(stdout, "» "+plain) {
		t.Fatalf("picker:\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	h.tty = false

	stdout = h.mustRun(t, "sync", "--all")
	if !strings.Contains(stdout, "  Friends client (Prism Launcher)\n  ✔ synced client") || !strings.Contains(stdout, "\n\n  friends client (MultiMC)\n") {
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
	h.mustRun(t, "sync", h.dir, "--side", "client", "--into", gameDir)
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
	if instances[0].LastSync != "" {
		t.Fatalf("a directory with no record of a sync is registered without a time: %+v", instances[0])
	}
	if f := readIntent(t, gameDir); f.Source != h.dir || f.Side != "client" {
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

func TestSyncStampsTheInstanceAndTheRow(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	overrides := filepath.Join(h.dir, "overrides")
	if err := os.MkdirAll(overrides, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overrides, "options.txt"), []byte("renderDistance:8\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends")
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")

	good := readInstances(t, h)[0]
	if good.LastSync == "" || good.LastError != "" {
		t.Fatalf("a sync that worked stamps lastSync and no error: %+v", good)
	}
	f := readIntent(t, gameDir)
	if f.Resolved == nil || f.Resolved.LastSyncAt != good.LastSync || f.Resolved.LastResult != instance.ResultOK {
		t.Fatalf("instance file: %+v", f.Resolved)
	}

	// A file changed on both sides conflicts, and the failure is recorded without disturbing the
	// time of the sync that built what is on disk.
	if err := os.WriteFile(filepath.Join(gameDir, "options.txt"), []byte("renderDistance:16\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overrides, "options.txt"), []byte("renderDistance:32\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := h.run(t, "sync", "-i", "friends", "--json")
	if code == 0 || failureCode(t, stdout).Code != "build-conflict" {
		t.Fatalf("a both-sides change should conflict: exit %d %s", code, stdout)
	}
	failed := readInstances(t, h)[0]
	if failed.LastSync != good.LastSync {
		t.Fatalf("a failed sync keeps the last good time: %+v", failed)
	}
	if !strings.Contains(failed.LastError, "changed in the output directory") {
		t.Fatalf("a failed sync records why: %+v", failed)
	}
	if f := readIntent(t, gameDir); f.Resolved.LastResult != instance.ResultFailed || f.Resolved.LastSyncAt != good.LastSync {
		t.Fatalf("instance file after a failure: %+v", f.Resolved)
	}
	if stdout := h.mustRun(t, "instances"); !strings.Contains(stdout, "last sync failed: "+failed.LastError) {
		t.Fatalf("instances should show the failure: %s", stdout)
	}

	// The registry rebuild reads the instance file, so a row it writes again knows when the
	// directory was last built correctly, whatever the sync after that did. The failure message
	// only ever lived on the row, and a rebuilt row goes without one.
	if err := os.WriteFile(registryPath(h), []byte("{ not a registry"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRunStderr(t, "instances", "repair", "--launcher", "prism", "--launcher-dir", prismDir)
	rebuilt := readInstances(t, h)[0]
	if rebuilt.LastSync != good.LastSync || rebuilt.LastError != "" {
		t.Fatalf("repair carries the last good sync and no error: %+v", rebuilt)
	}

	h.mustRun(t, "sync", "-i", rebuilt.ID, "--force")
	if in := readInstances(t, h)[0]; in.LastError != "" {
		t.Fatalf("a sync that works clears the error: %+v", in)
	}
}

func TestInstancesShowsALaunchThatNeverStarted(t *testing.T) {
	h, gameDir, _ := wrappedInstance(t, "0", nil)
	java := readIntent(t, gameDir).Resolved.Java
	if err := os.Remove(java); err != nil {
		t.Fatal(err)
	}
	if code, _, _ := h.run(t, "hook", "wrap", "-C", gameDir, "--", "--gameDir", gameDir, "--accessToken", accessToken); code == 0 {
		t.Fatal("a launch that never started exits non-zero")
	}

	var env struct {
		Data []instanceEntry `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "instances", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data) != 1 || !strings.Contains(env.Data[0].LaunchError, java) {
		t.Fatalf("the entry should carry the reason the launch never started: %+v", env.Data)
	}
	stdout := h.mustRun(t, "instances")
	if !strings.Contains(stdout, "last launch didn't start: ") || !strings.Contains(stdout, java) {
		t.Fatalf("instances should show the failed launch:\n%s", stdout)
	}
	if strings.Contains(stdout, accessToken) {
		t.Fatalf("instances repeats the game argv:\n%s", stdout)
	}

	h.mustRun(t, "instances", "repair")
	fresh := readIntent(t, gameDir)
	fresh.Resolved.Java, _ = fakeGameExe(t, "0")
	if err := fresh.Save(gameDir); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "hook", "wrap", "-C", gameDir, "--", "--gameDir", gameDir)
	if stdout := h.mustRun(t, "instances"); strings.Contains(stdout, "last launch didn't start") {
		t.Fatalf("a launch that started clears the line:\n%s", stdout)
	}
}
