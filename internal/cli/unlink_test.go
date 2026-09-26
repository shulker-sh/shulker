package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/manifest"
)

func unlinkJSON(t *testing.T, h *harness, args ...string) []unlinkResult {
	t.Helper()
	var env struct {
		Data []unlinkResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, append(append([]string{"unlink"}, args...), "--json")...)), &env); err != nil {
		t.Fatal(err)
	}
	return env.Data
}

func TestUnlink(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends")
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir, "--name", "Custom")
	mojangDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(mojangDir, launcher.ProfilesFile), []byte(`{"profiles":{"other":{"name":"Other","gameDir":"/elsewhere"}}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "link", "mojang", "--launcher-dir", mojangDir)

	code, stdout, _ := h.run(t, "unlink", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "usage" || !strings.Contains(e.Help, "shulker instances") {
		t.Fatalf("unlink with no name: exit %d %s", code, stdout)
	}
	code, stdout, _ = h.run(t, "unlink", "nope", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "instance-not-found" || len(e.Candidates) != 3 {
		t.Fatalf("unknown name: exit %d %s", code, stdout)
	}

	friendsDir := filepath.Join(prismDir, "instances", "shulker-friends")
	r := unlinkJSON(t, h, "friends")
	if len(r) != 1 || r[0].Removed != launcher.RemovedPreLaunch || r[0].Relink != "shulker link prism "+h.dir+" --name Friends --launcher-dir "+prismDir || r[0].RelinkIn != "" {
		t.Fatalf("unlink prism: %+v", r)
	}
	if cfg := readINIFile(t, filepath.Join(friendsDir, launcher.PrismInstanceFile)); cfg["PreLaunchCommand"] != "" || cfg["name"] != "Friends" {
		t.Fatalf("instance.cfg after unlink: %+v", cfg)
	}
	if _, err := os.Stat(filepath.Join(friendsDir, "minecraft", "mods")); err != nil {
		t.Fatalf("unlink must keep the instance's files: %v", err)
	}

	customCfg := filepath.Join(prismDir, "instances", "shulker-custom", launcher.PrismInstanceFile)
	data, _ := os.ReadFile(customCfg)
	custom := strings.Replace(string(data), "PreLaunchCommand=", "PreLaunchCommand=echo hi\nOldPreLaunch=", 1)
	if err := os.WriteFile(customCfg, []byte(custom), 0o644); err != nil {
		t.Fatal(err)
	}
	// The pre-launch command isn't shulker's, so it stays. The post-exit slot is shulker's, so it
	// goes, and unlink reports the slot that actually went.
	if r := unlinkJSON(t, h, "Custom"); r[0].Removed != launcher.RemovedPostExit || readINIFile(t, customCfg)["PreLaunchCommand"] != "echo hi" {
		t.Fatalf("a pre-launch command that isn't shulker's is kept: %+v", r)
	}

	r = unlinkJSON(t, h, "pack", "--launcher", "mojang")
	if r[0].Removed != launcher.RemovedProfile || r[0].RelinkIn != "" || r[0].Relink != "shulker link mojang "+h.dir+" --name pack --launcher-dir "+mojangDir {
		t.Fatalf("unlink mojang: %+v", r)
	}
	var profiles struct {
		Profiles map[string]any `json:"profiles"`
	}
	readJSONFile(t, filepath.Join(mojangDir, launcher.ProfilesFile), &profiles)
	if len(profiles.Profiles) != 1 || profiles.Profiles["other"] == nil {
		t.Fatalf("only shulker's profile is removed: %+v", profiles.Profiles)
	}

	if links := readInstances(t, h); len(links) != 0 {
		t.Fatalf("every entry was unlinked: %+v", links)
	}
}

func TestUnlinkLauncherNameInProject(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	mojangDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(mojangDir, launcher.ProfilesFile), []byte(`{"profiles":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "link", "mojang", "--launcher-dir", mojangDir)
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir, "--name", "A")
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir, "--name", "B")
	h.mustRun(t, "link", "multimc", h.dir, "--launcher-dir", t.TempDir(), "--name", "atlauncher")
	named := instanceDir(t, h, "atlauncher")

	if r := unlinkJSON(t, h, "vanilla"); len(r) != 1 || r[0].Removed != launcher.RemovedProfile {
		t.Fatalf("unlink vanilla in the project: %+v", r)
	}
	code, stdout, _ := h.run(t, "unlink", "prism", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "ambiguous-instance" || len(e.Candidates) != 2 {
		t.Fatalf("two prism entries: exit %d %s", code, stdout)
	}
	if r := unlinkJSON(t, h, "prism", "--all"); len(r) != 2 {
		t.Fatalf("unlink prism --all: %+v", r)
	}
	if r := unlinkJSON(t, h, "atlauncher"); len(r) != 1 || r[0].Dir != named {
		t.Fatalf("an entry named like a launcher comes first: %+v", r)
	}
}

func TestUnlinkAll(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "link", "multimc", h.dir, "--launcher-dir", t.TempDir(), "--name", "Twin")
	h.mustRun(t, "link", "multimc", h.dir, "--launcher-dir", t.TempDir(), "--name", "Twin")
	h.mustRun(t, "link", "prism", "--launcher-dir", t.TempDir(), "--name", "Twin")

	code, stdout, _ := h.run(t, "unlink", "Twin", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "ambiguous-instance" || len(e.Candidates) != 3 {
		t.Fatalf("ambiguous name: exit %d %s", code, stdout)
	}
	if r := unlinkJSON(t, h, "Twin", "--all", "--side", "client", "--launcher", "prism"); len(r) != 1 || r[0].Launcher != "prism" {
		t.Fatalf("--launcher narrows --all: %+v", r)
	}
	if r := unlinkJSON(t, h, "--all"); len(r) != 2 {
		t.Fatalf("--all alone unlinks everything left: %+v", r)
	}
	if links := readInstances(t, h); len(links) != 0 {
		t.Fatalf("registry after --all: %+v", links)
	}
}

func TestUnlinkedInstanceStaysUnlinked(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir, "--as", "friends")
	gameDir := filepath.Join(prismDir, "instances", "shulker-pack", "minecraft")
	h.mustRun(t, "unlink", "friends")
	if f := readIntent(t, gameDir); !f.IsUnlinked {
		t.Fatalf("unlink should mark the instance file: %+v", f)
	}

	// The mark outranks every other reading of the directory, including the manifest that would
	// otherwise name it an instance on its own.
	if _, err := os.Stat(filepath.Join(gameDir, manifest.FileName)); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "instances", "repair", "--launcher", "prism", "--launcher-dir", prismDir)
	if instances := readInstances(t, h); len(instances) != 0 {
		t.Fatalf("repair must not register an unlinked instance again: %+v", instances)
	}

	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir, "--as", "friends")
	if f := readIntent(t, gameDir); f.IsUnlinked {
		t.Fatalf("link should clear the mark: %+v", f)
	}
}

// An unlink leaves a whole project behind, and the hint it prints picks that same folder back up.
// The registry row is gone by then, so the manifest is the only thing that can say what the
// instance follows — and a link that disagrees with it still has to ask for --force.
func TestUnlinkThenLinkAdoptsTheSameFolder(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	other := filepath.Join(t.TempDir(), "other")
	lockedPack(t, h, other, `"fabric-api": {}`)

	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir, "--name", "Friends")
	instDir := filepath.Join(prismDir, "instances", "shulker-friends")
	gameDir := filepath.Join(instDir, "minecraft")
	h.mustRun(t, "-C", gameDir, "add", "fresh-animations")

	r := unlinkJSON(t, h, "Friends")
	if len(r) != 1 || r[0].Relink != "shulker link prism "+h.dir+" --name Friends --launcher-dir "+prismDir {
		t.Fatalf("the hint's source comes from the manifest: %+v", r)
	}

	code, stdout, _ := h.run(t, "link", "prism", other, "--launcher-dir", prismDir, "--name", "Friends", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "instance-exists" || !strings.Contains(e.Message, h.dir) || !strings.Contains(e.Help, "repoint the modpack it follows") {
		t.Fatalf("a source the unlinked project doesn't follow: exit %d %s", code, stdout)
	}

	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends")
	if f := readIntent(t, gameDir); f.IsUnlinked {
		t.Fatalf("adopting clears the unlinked mark: %+v", f)
	}
	if instances := readInstances(t, h); len(instances) != 1 || instances[0].Dir != gameDir {
		t.Fatalf("adopting registers the same folder again: %+v", instances)
	}
	if cfg := readINIFile(t, filepath.Join(instDir, launcher.PrismInstanceFile)); cfg["PreLaunchCommand"] == "" {
		t.Fatalf("adopting rewrites the launcher's slots: %+v", cfg)
	}
	requires, _ := instanceManifest(t, gameDir)["requires"].(map[string]any)
	entry, _ := requires["pack"].(map[string]any)
	if len(requires) != 2 || entry["source"] != h.dir || requires["fresh-animations"] == nil {
		t.Fatalf("adoption leaves the manifest as it found it: %v", requires)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "resourcepacks", "FreshAnimations_v1.9.4.zip")); err != nil {
		t.Fatalf("what the player added is still there: %v", err)
	}

	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends", "--as", "smp")
	if instances := readInstances(t, h); len(instances) != 1 || instances[0].ID != "smp" || instanceManifest(t, gameDir)["name"] != "smp" {
		t.Fatalf("an adopted project's name is the id it is linked under, which repair reads back: %+v", instances)
	}
}

func TestUnlinkDetachedBuild(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	into := filepath.Join(t.TempDir(), "detached")
	h.mustRun(t, "sync", h.dir, "--into", into)
	if lf, _ := local.Load(h.dir); len(lf.SyncDirs["client"]) != 1 {
		t.Fatalf("sync --into records the directory: %+v", lf.SyncDirs)
	}

	r := unlinkJSON(t, h, into)
	if len(r) != 1 || r[0].Dir != into || r[0].Relink != "shulker sync "+h.dir+" --side client --into "+into {
		t.Fatalf("unlink a detached build: %+v", r)
	}
	if lf, _ := local.Load(h.dir); len(lf.SyncDirs["client"]) != 0 {
		t.Fatalf("unlink drops the directory from syncDirs: %+v", lf.SyncDirs)
	}
	if inf, err := instance.Load(into); err != nil || !inf.IsUnlinked {
		t.Fatalf("unlink marks the detached build unlinked: %+v %v", inf, err)
	}
	if _, err := os.Stat(instance.StatePath(into)); err != nil {
		t.Fatalf("unlink keeps the detached build's files: %v", err)
	}

	code, stdout, _ := h.run(t, "unlink", into, "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "no-instances" && e.Code != "instance-not-found" {
		t.Fatalf("unlinking it again: exit %d %s", code, stdout)
	}
}

func TestUnlinkMentionsOnlyAPreLaunchCommandThatExists(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends", "--no-hooks")

	stdout := h.mustRun(t, "unlink", "friends")
	if strings.Contains(stdout, "pre-launch") || !strings.Contains(stdout, "Unlinked Friends from Prism Launcher") || !strings.Contains(stdout, "Kept the instance folder and its worlds") {
		t.Fatalf("unlink of an instance with no hooks: %s", stdout)
	}
}
