package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/project"
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
	if f := readIntent(t, gameDir); f.Source != "" || f.Side != "" {
		t.Fatalf("an in-place instance keeps neither in its file: %+v", f)
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

// An in-place instance's file keeps what the manifest can't hold and nothing else, and every
// reader takes the manifest as its fallback.
func TestInPlaceInstanceFileKeepsSettingsOnly(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	gitRun(t, h.dir, "init", "-q", "-b", "main")
	gitRun(t, h.dir, "add", ".")
	gitRun(t, h.dir, "commit", "-q", "-m", "one")
	source := "file://" + h.dir

	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", source, "--launcher-dir", prismDir, "--name", "Friends", "--ref", "main")
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")

	var raw map[string]any
	readJSONFile(t, instance.Path(gameDir), &raw)
	for _, key := range []string{"source", "ref", "side", "assumeClient"} {
		if _, held := raw[key]; held {
			t.Fatalf("the manifest holds %s, so the instance file writes none: %v", key, raw)
		}
	}
	f := readIntent(t, gameDir)
	if !f.Settings.PreLaunch() || !f.Settings.PostExit() || f.Resolved == nil || f.Resolved.LastResult != instance.ResultOK {
		t.Fatalf("what is left is the settings and the last sync: %+v", f)
	}

	// A registry row that fell behind can't misreport what the instance follows: the manifest is
	// the one writer of all three.
	instances := readInstances(t, h)
	instances[0].Source = filepath.Join(t.TempDir(), "moved-away")
	if _, err := config.WriteInstances(registryPath(h), instances); err != nil {
		t.Fatal(err)
	}
	var env struct {
		Data []project.InstanceEntry `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "instances", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data) != 1 {
		t.Fatalf("one instance: %+v", env.Data)
	}
	if e := env.Data[0]; e.Source != source || e.Ref != "main" || e.Side != "client" {
		t.Fatalf("instances reads the manifest: %+v", e)
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
		Data []project.InstanceEntry `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "instances", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, e := range env.Data {
		got = append(got, e.Launcher+":"+e.ID+":"+e.Status)
	}
	want := "prism:alpha:synced prism:gone:missing prism:locked:unreadable prism:zed:synced mojang:pack:synced"
	if strings.Join(got, " ") != want {
		t.Fatalf("instances:\n got %s\nwant %s", strings.Join(got, " "), want)
	}
	// Every link builds, so a linked instance is synced from the moment it exists.
	if env.Data[0].SyncedAt == "" || env.Data[3].SyncedAt == "" {
		t.Fatalf("syncedAt comes from the state file: %+v", env.Data)
	}

	stdout := h.mustRun(t, "instances")
	rows := tableRows(stdout)
	if len(rows) != 5 {
		t.Fatalf("instances is one table across launchers:\n%s", stdout)
	}
	for i, want := range []map[string]string{
		{"": "•", "Instance": "alpha", "Launcher": "prism", "Side": "client", "Status": "synced "},
		{"": "•", "Instance": "gone", "Launcher": "prism", "Side": "", "Status": "directory is missing"},
		{"": "•", "Instance": "locked", "Launcher": "prism", "Side": "", "Status": "can't read the directory"},
		{"": "•", "Instance": "zed", "Launcher": "prism", "Side": "client", "Status": "synced ", "Path": filepath.Join(prismDir, "instances", "shulker-zed", "minecraft")},
		{"": "•", "Instance": "pack", "Launcher": "mojang", "Side": "client", "Status": "synced "},
	} {
		for header, value := range want {
			if got := rows[i][header]; !strings.HasPrefix(got, value) {
				t.Fatalf("row %d %s = %q, want %q:\n%s", i, header, got, value, stdout)
			}
		}
	}
	if strings.Contains(stdout, "Zed, from") || strings.Contains(stdout, "ref ") {
		t.Fatalf("the source and ref are left to --json:\n%s", stdout)
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

	// The picker draws on the screen and reads keys, so it needs a terminal on both ends; a test's
	// buffers are neither, and a run that can't ask says which instances matched instead.
	h.tty, h.stdin = true, strings.NewReader("2\n")
	code, stdout, _ := h.run(t, "sync", "-C", t.TempDir(), "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "ambiguous-instance" || len(e.Candidates) != 2 {
		t.Fatalf("off a terminal sync should name both instances: %s", stdout)
	}
	h.tty = false

	stdout = h.mustRun(t, "sync", "--all")
	if !strings.Contains(stdout, "  Friends friends client (Prism Launcher)\n  ✔ synced client") || !strings.Contains(stdout, "\n\n  friends friends-2 client (MultiMC)\n") {
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
	code, stdout, _ = h.run(t, "sync", "--all", "--json")
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
	if rows := tableRows(h.mustRun(t, "instances")); len(rows) != 1 || rows[0]["Launcher"] != "prism" {
		t.Fatalf("instances names its launcher: %+v", rows)
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

	// A registry shulker can't read is rebuilt from what the launcher holds, and an instance
	// missing its file gets one holding what the manifest can't.
	if err := os.WriteFile(registryPath(h), []byte("{ not a registry"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(instance.Path(gameDir)); err != nil {
		t.Fatal(err)
	}
	stdout, stderr := h.mustRunStderr(t, "instances", "repair", "--launcher", "prism", "--launcher-dir", prismDir)
	if !strings.Contains(stderr, "rebuilt it") || !strings.Contains(stdout, "registered ") {
		t.Fatalf("repair:\nstdout: %s\nstderr: %s", stdout, stderr)
	}
	instances := readInstances(t, h)
	if len(instances) != 1 || instances[0].Dir != gameDir || instances[0].Launcher != "prism" {
		t.Fatalf("the scan registers the instance again: %+v", instances)
	}
	if instances[0].LastSync != "" {
		t.Fatalf("a directory with no record of a sync is registered without a time: %+v", instances[0])
	}
	if f := readIntent(t, gameDir); f.Source != "" || f.Ref != "" || f.Side != "" || !f.Settings.PreLaunch() {
		t.Fatalf("repair writes a defaults-only instance file for a project: %+v", f)
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

// Repair rewrites a registry or instance file it can't read, a newer shulker's included, so it keeps
// the old bytes as <name>.replaced first.
func TestInstancesRepairKeepsWhatItReplaces(t *testing.T) {
	h := newHarness(t)
	prismDir := t.TempDir()
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends")
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")

	oldRegistry := []byte(`{"$schema":"https://shulker.sh/schema/v2/registry.json","instances":[]}`)
	oldIntent := []byte(`{"$schema":"https://shulker.sh/schema/v2/instance.json"}`)
	if err := os.WriteFile(registryPath(h), oldRegistry, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(instance.Path(gameDir), oldIntent, 0o644); err != nil {
		t.Fatal(err)
	}
	_, stderr := h.mustRunStderr(t, "instances", "repair", "--launcher", "prism", "--launcher-dir", prismDir)
	for path, want := range map[string][]byte{registryPath(h) + ".replaced": oldRegistry, instance.Path(gameDir) + ".replaced": oldIntent} {
		if got, err := os.ReadFile(path); err != nil || string(got) != string(want) {
			t.Fatalf("%s keeps the old bytes: %q %v", path, got, err)
		}
		if !strings.Contains(stderr, "kept the old one as "+path) {
			t.Fatalf("repair names where the old file went: %s", stderr)
		}
	}
	if instances := readInstances(t, h); len(instances) != 1 {
		t.Fatalf("the registry is rebuilt: %+v", instances)
	}
	readIntent(t, gameDir)
}

// Under ADR 0001 a project building where it stands in a launcher's game directory is an instance,
// so repair recognises one from its manifest alone: no .shulker/, no row, nothing but the project
// the player kept.
func TestInstancesRepairRecognisesAnInPlaceProject(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Lost")
	instDir := filepath.Join(prismDir, "instances", "shulker-lost")
	gameDir := filepath.Join(instDir, "minecraft")

	// Everything shulker wrote outside the project is gone: its .shulker directory, its row, and
	// the slot command the link installed.
	if err := os.RemoveAll(filepath.Join(gameDir, instance.Dir)); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(instDir, launcher.PrismInstanceFile)
	var kept []string
	for _, line := range strings.Split(readFile(t, cfgPath), "\n") {
		if !strings.HasPrefix(line, "PreLaunchCommand") && !strings.HasPrefix(line, "OverrideCommands") {
			kept = append(kept, line)
		}
	}
	writeFile(t, cfgPath, strings.Join(kept, "\n"))
	if _, err := config.WriteInstances(registryPath(h), nil); err != nil {
		t.Fatal(err)
	}

	if stdout := h.mustRun(t, "instances", "repair", "--launcher", "prism", "--launcher-dir", prismDir); !strings.Contains(stdout, "registered lost") {
		t.Fatalf("repair registers the directory again, under the id it was linked as: %s", stdout)
	}
	instances := readInstances(t, h)
	if len(instances) != 1 || instances[0].Dir != gameDir || instances[0].Source != h.dir {
		t.Fatalf("the source comes from the modpack the manifest requires: %+v", instances)
	}
	if instances[0].Name != "Lost" {
		t.Fatalf("the name is the one the launcher shows, not the folder's: %+v", instances[0])
	}
	if instances[0].ID != "lost" {
		t.Fatalf("the id is the manifest's name, which the link set to the id: %+v", instances[0])
	}

	// The instance file it writes holds nothing the manifest holds.
	var raw map[string]any
	readJSONFile(t, instance.Path(gameDir), &raw)
	for _, key := range []string{"source", "ref", "side"} {
		if _, held := raw[key]; held {
			t.Fatalf("a repaired project keeps no %s in its instance file: %v", key, raw)
		}
	}
	if f := readIntent(t, gameDir); !f.Settings.PreLaunch() || !f.Settings.PostExit() {
		t.Fatalf("the settings are written at their defaults: %+v", f)
	}

	// Repair writes rows and instance files and no slot command, so the rediscovered instance is
	// listed and syncable by -i while its pre-launch refresh waits for a link.
	if cfg := readINIFile(t, cfgPath); cfg["PreLaunchCommand"] != "" {
		t.Fatalf("repair installs no slot command: %q", cfg["PreLaunchCommand"])
	}
	if _, err := os.Stat(filepath.Join(gameDir, instance.Dir, "pre-launch")); !os.IsNotExist(err) {
		t.Fatalf("repair generates no hook script: %v", err)
	}
	if stdout := h.mustRun(t, "instances"); !strings.Contains(stdout, "shulker-lost") {
		t.Fatalf("instances lists it: %s", stdout)
	}
	h.mustRun(t, "sync", "-i", "lost")
}

// Every launcher's folder name is a mangled form of the name it shows, so a rebuilt registry reads
// the name back from the file each one keeps it in.
func TestInstancesRepairNamesAnInstanceTheWayItsLauncherShowsIt(t *testing.T) {
	h := newHarness(t)
	shulkerInstances(t, h)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	dirs := map[string]string{}
	for _, name := range []string{"prism", "atlauncher", "gdlauncher", "mojang"} {
		dir, err := filepath.EvalSymlinks(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		dirs[name] = dir
		h.mustRun(t, "link", name, h.dir, "--launcher-dir", dir, "--name", "Friends Pack", "--as", name)
	}
	h.mustRun(t, "link", "shulker", h.dir, "--as", "smp")

	if _, err := config.WriteInstances(registryPath(h), nil); err != nil {
		t.Fatal(err)
	}
	for name, dir := range dirs {
		h.mustRun(t, "instances", "repair", "--launcher", name, "--launcher-dir", dir)
	}
	h.mustRun(t, "instances", "repair", "--launcher", "shulker")

	instances := readInstances(t, h)
	if len(instances) != 5 {
		t.Fatalf("every instance is registered again: %+v", instances)
	}
	for _, in := range instances {
		want := "Friends Pack"
		if in.Launcher == "shulker" {
			want = "pack"
		}
		if in.Name != want {
			t.Errorf("%s: the row is named %q, want %q", in.Launcher, in.Name, want)
		}
	}
}

// Renaming an instance in the launcher is how a player renames it, so repair follows the launcher's
// file and says so. The id is what scripts and -i use, so it stays.
func TestInstancesRepairFollowsARenameInTheLauncher(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends")
	cfgPath := filepath.Join(prismDir, "instances", "shulker-friends", launcher.PrismInstanceFile)

	rename := func(name string) {
		lines := strings.Split(readFile(t, cfgPath), "\n")
		for i, line := range lines {
			if strings.HasPrefix(line, "name=") {
				lines[i] = "name=" + strconv.Quote(name)
			}
		}
		writeFile(t, cfgPath, strings.Join(lines, "\n"))
	}
	repairJSON := func() repairResult {
		var env struct {
			Data repairResult `json:"data"`
		}
		if err := json.Unmarshal([]byte(h.mustRun(t, "instances", "repair", "--json")), &env); err != nil {
			t.Fatal(err)
		}
		return env.Data
	}

	rename("Friends SMP")
	gameDir := filepath.Join(filepath.Dir(cfgPath), "minecraft")
	if res := repairJSON(); len(res.Renamed) != 1 || res.Renamed[0] != (repairRename{ID: "friends", Dir: gameDir, From: "Friends", To: "Friends SMP"}) {
		t.Fatalf("repair reports the rename: %+v", res.Renamed)
	}
	instances := readInstances(t, h)
	if len(instances) != 1 || instances[0].Name != "Friends SMP" || instances[0].ID != "friends" {
		t.Fatalf("the row takes the new name and keeps its id: %+v", instances)
	}
	if res := repairJSON(); len(res.Renamed) != 0 {
		t.Fatalf("a name already in step is no rename: %+v", res.Renamed)
	}

	rename("Friends Survival")
	if stdout := h.mustRun(t, "instances", "repair"); !strings.Contains(stdout, "renamed friends  Friends SMP ⟶ Friends Survival") {
		t.Fatalf("repair prints the rename: %s", stdout)
	}

	// A launcher file with nothing to read never blanks the name unlink needs.
	writeFile(t, cfgPath, "")
	h.mustRun(t, "instances", "repair")
	if instances := readInstances(t, h); instances[0].Name != "Friends Survival" {
		t.Fatalf("an unreadable name keeps the row's: %+v", instances[0])
	}
}

// A directory shulker only syncs into is no project, so its source is its instance file's, and the
// state the last build left where the file is gone too.
func TestInstancesRepairReadsASyncedDirectory(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	prismDir := t.TempDir()
	gameDir := filepath.Join(prismDir, "instances", "handmade", "minecraft")
	h.mustRun(t, "sync", h.dir, "--side", "client", "--into", gameDir)

	h.mustRun(t, "instances", "repair", "--launcher", "prism", "--launcher-dir", prismDir)
	instances := readInstances(t, h)
	if len(instances) != 1 || instances[0].Dir != gameDir || instances[0].Source != h.dir {
		t.Fatalf("a synced directory is registered from its instance file: %+v", instances)
	}

	// With the instance file gone too, what the last build recorded is the only reading left, and
	// repair writes the file again from it.
	if err := os.Remove(instance.Path(gameDir)); err != nil {
		t.Fatal(err)
	}
	if _, err := config.WriteInstances(registryPath(h), nil); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "instances", "repair", "--launcher", "prism", "--launcher-dir", prismDir)
	if instances := readInstances(t, h); len(instances) != 1 || instances[0].Source != h.dir {
		t.Fatalf("a synced directory with no instance file is registered from its build state: %+v", instances)
	}
	if f := readIntent(t, gameDir); f.Source != h.dir || f.Side != "client" {
		t.Fatalf("repair writes the instance file from what the build recorded: %+v", f)
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
		Data []project.InstanceEntry `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "instances", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data) != 1 || !strings.Contains(env.Data[0].LaunchError, java) {
		t.Fatalf("the entry should carry the reason the launch never started: %+v", env.Data)
	}
	stdout := h.mustRun(t, "instances")
	if rows := tableRows(stdout); len(rows) != 1 || !strings.Contains(squash(rows[0]["Status"]), "lastlaunchdidn'tstart:") || !strings.Contains(squash(rows[0]["Status"]), squash(java)) {
		t.Fatalf("instances should show the failed launch under its status:\n%s", stdout)
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

func TestInstancesRepairKeepsTheIDALinkChose(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir, "--as", "mine")

	h.mustRun(t, "instances", "repair", "--launcher", "prism", "--launcher-dir", prismDir)
	if in := readInstances(t, h); len(in) != 1 || in[0].ID != "mine" {
		t.Fatalf("a repair over a whole registry keeps the id: %+v", in)
	}

	if err := os.Remove(registryPath(h)); err != nil {
		t.Fatal(err)
	}
	stdout := h.mustRun(t, "instances", "repair", "--launcher", "prism", "--launcher-dir", prismDir)
	if !strings.Contains(stdout, "registered mine") {
		t.Fatalf("a repair from scratch reads the id back from the manifest, not the folder: %s", stdout)
	}
	if in := readInstances(t, h); len(in) != 1 || in[0].ID != "mine" || in[0].Launcher != "prism" {
		t.Fatalf("rebuilt row: %+v", in)
	}
}

func TestInstancesRepairWithoutARegistryFindsShulkersOwnInstances(t *testing.T) {
	h := newHarness(t)
	root := shulkerInstances(t, h)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "link", "shulker", "--as", "smp")
	if err := os.Remove(registryPath(h)); err != nil {
		t.Fatal(err)
	}

	stdout := h.mustRun(t, "instances", "repair")
	if !strings.Contains(stdout, "registered smp") {
		t.Fatalf("a bare repair scans the instances root with the launchers: %s", stdout)
	}
	if in := readInstances(t, h); len(in) != 1 || in[0].ID != "smp" || in[0].Launcher != "shulker" || in[0].Dir != filepath.Join(root, "smp") {
		t.Fatalf("rebuilt row: %+v", in)
	}
}
