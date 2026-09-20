package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/launcher"
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
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
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
	if e := failureCode(t, stdout); code == 0 || e.Code != "usage" || !strings.Contains(e.Message, "shulker instances") {
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
	if cfg := readINIFile(t, filepath.Join(friendsDir, launcher.InstanceConfigFile)); cfg["PreLaunchCommand"] != "" || cfg["name"] != "Friends" {
		t.Fatalf("instance.cfg after unlink: %+v", cfg)
	}
	if _, err := os.Stat(filepath.Join(friendsDir, "minecraft", "mods")); err != nil {
		t.Fatalf("unlink must keep the instance's files: %v", err)
	}

	customCfg := filepath.Join(prismDir, "instances", "shulker-custom", launcher.InstanceConfigFile)
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
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
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
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
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
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir, "--as", "friends")
	gameDir := filepath.Join(prismDir, "instances", "shulker-pack", "minecraft")
	h.mustRun(t, "unlink", "friends")
	if f := readIntent(t, gameDir); !f.Unlinked {
		t.Fatalf("unlink should mark the instance file: %+v", f)
	}

	h.mustRun(t, "instances", "repair", "--launcher", "prism", "--launcher-dir", prismDir)
	if instances := readInstances(t, h); len(instances) != 0 {
		t.Fatalf("repair must not register an unlinked instance again: %+v", instances)
	}

	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir, "--as", "friends")
	if f := readIntent(t, gameDir); f.Unlinked {
		t.Fatalf("link should clear the mark: %+v", f)
	}
}
