package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/manifest"
)

func TestLinkPrismFromRemoteSource(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "my-pack")
	h.mustRun(t, "add", "sodium")
	setMod(t, h, "sodium", map[string]any{"feature": "fancy"})
	gitRun(t, h.dir, "init", "-q", "-b", "main")
	gitRun(t, h.dir, "add", ".")
	gitRun(t, h.dir, "commit", "-q", "-m", "one")
	source := "file://" + h.dir

	launcherDir := t.TempDir()
	var env struct {
		Data prismReport `json:"data"`
	}
	stdout := h.mustRun(t, "link", "prism", source, "--launcher-dir", launcherDir, "--name", "Friends", "--with", "fancy", "--ref", "main", "--json")
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	instDir := filepath.Join(launcherDir, "instances", "shulker-friends")
	gameDir := filepath.Join(instDir, "minecraft")
	if rep := env.Data; rep.Source != source || rep.Name != "Friends" || rep.GameDir != gameDir || rep.Sync == nil {
		t.Fatalf("link report: %+v", rep)
	}
	// The slot names the generated script through Prism's own token, so it carries neither the
	// source nor the binary's path.
	wantCmd := `sh "$INST_MC_DIR/.shulker/pre-launch"`
	if cfg := readINIFile(t, filepath.Join(instDir, launcher.InstanceConfigFile)); cfg["PreLaunchCommand"] != wantCmd {
		t.Fatalf("PreLaunchCommand = %q, want %q", cfg["PreLaunchCommand"], wantCmd)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("the first sync should ship the mod turned on by --with: %v", err)
	}
	if st := build.LoadState(gameDir); st.Source != source || st.Ref != "main" {
		t.Fatalf("state origin: %+v", st.Origin)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build")); !os.IsNotExist(err) {
		t.Fatalf("linking a remote source must not build in the current directory: %v", err)
	}

	h.mustRun(t, "link", "prism", source, "--launcher-dir", launcherDir, "--name", "Friends")
	code, stdout, _ := h.run(t, "link", "prism", "--launcher-dir", launcherDir, "--name", "Friends", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "instance-exists" || !strings.Contains(e.Message, source) {
		t.Fatalf("linking another source into the instance: exit %d %s", code, stdout)
	}
	h.mustRun(t, "link", "prism", "--launcher-dir", launcherDir, "--name", "Friends", "--force")
	// The slot is a fixed script path now, so where the instance syncs from shows in its own file.
	if intent := readFile(t, filepath.Join(instDir, "minecraft", instance.Dir, instance.FileName)); !strings.Contains(intent, h.dir) {
		t.Fatalf("--force should repoint the instance: %s", intent)
	}

	code, stdout, _ = h.run(t, "link", "prism", "--launcher-dir", launcherDir, "--ref", "main", "--json")
	if code == 0 || failureCode(t, stdout).Code != "usage" {
		t.Fatalf("--ref without a source: exit %d %s", code, stdout)
	}
	code, stdout, _ = h.run(t, "link", "prism", source, "--launcher-dir", launcherDir, "--mode", "symlink", "--json")
	if code == 0 || failureCode(t, stdout).Code != "usage" {
		t.Fatalf("symlink from a remote source: exit %d %s", code, stdout)
	}
}

func TestLinkPrism(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "my-pack")
	h.mustRun(t, "add", "sodium")

	launcherDir := t.TempDir()
	stdout := h.mustRun(t, "link", "prism", "--launcher-dir", launcherDir)
	instDir := filepath.Join(launcherDir, "instances", "shulker-my-pack")
	if !strings.Contains(stdout, "created instance my-pack » "+instDir) || !strings.Contains(stdout, "before each launch") {
		t.Fatalf("link output: %s", stdout)
	}
	cfg := readINIFile(t, filepath.Join(instDir, launcher.InstanceConfigFile))
	if cfg["InstanceType"] != "OneSix" || cfg["name"] != "my-pack" || cfg["OverrideCommands"] != "true" {
		t.Fatalf("instance.cfg: %v", cfg)
	}
	wantCmd := `sh "$INST_MC_DIR/.shulker/pre-launch"`
	if cfg["PreLaunchCommand"] != wantCmd {
		t.Fatalf("PreLaunchCommand = %q, want %q", cfg["PreLaunchCommand"], wantCmd)
	}
	if info, err := os.Lstat(filepath.Join(instDir, "minecraft")); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("minecraft dir: %v %v", info, err)
	}
	var pack struct {
		Components    []map[string]any `json:"components"`
		FormatVersion int              `json:"formatVersion"`
	}
	readJSONFile(t, filepath.Join(instDir, launcher.PackFile), &pack)
	if pack.FormatVersion != 1 || len(pack.Components) != 2 ||
		pack.Components[0]["uid"] != "net.minecraft" || pack.Components[0]["version"] != "26.2" || pack.Components[0]["important"] != true ||
		pack.Components[1]["uid"] != "net.fabricmc.fabric-loader" || pack.Components[1]["version"] != "0.17.3" {
		t.Fatalf("mmc-pack.json: %+v", pack)
	}

	extra := "[General]\nInstanceType=OneSix\nname=Old\nlastLaunchTime=5\nJavaPath=/usr/bin/java\n"
	if err := os.WriteFile(filepath.Join(instDir, launcher.InstanceConfigFile), []byte(extra), 0o644); err != nil {
		t.Fatal(err)
	}
	packWithLWJGL := `{"components":[{"uid":"net.minecraft","version":"26.1","important":true},{"uid":"org.lwjgl3","version":"3.3.3","dependencyOnly":true},{"uid":"org.quiltmc.quilt-loader","version":"0.1"}],"formatVersion":1}`
	if err := os.WriteFile(filepath.Join(instDir, launcher.PackFile), []byte(packWithLWJGL), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout = h.mustRun(t, "link", "prism", "--launcher-dir", launcherDir, "--mode", "symlink")
	if !strings.Contains(stdout, "updated instance") || !strings.Contains(stdout, "restart the launcher") || !strings.Contains(stdout, "$ shulker install") {
		t.Fatalf("second link output: %s", stdout)
	}
	cfg = readINIFile(t, filepath.Join(instDir, launcher.InstanceConfigFile))
	if cfg["name"] != "my-pack" || cfg["lastLaunchTime"] != "5" || cfg["JavaPath"] != "/usr/bin/java" || cfg["PreLaunchCommand"] != "" {
		t.Fatalf("instance.cfg after symlink mode: %v", cfg)
	}
	link, err := os.Readlink(filepath.Join(instDir, "minecraft"))
	if err != nil || link != filepath.Join(h.dir, "build", "client") {
		t.Fatalf("minecraft link = %q, %v", link, err)
	}
	readJSONFile(t, filepath.Join(instDir, launcher.PackFile), &pack)
	if len(pack.Components) != 3 || pack.Components[0]["version"] != "26.2" || pack.Components[1]["uid"] != "org.lwjgl3" || pack.Components[2]["uid"] != "net.fabricmc.fabric-loader" {
		t.Fatalf("merged mmc-pack.json: %+v", pack)
	}

	var env struct {
		Data prismReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "link", "prism", "--launcher-dir", launcherDir, "--json")), &env); err != nil {
		t.Fatal(err)
	}
	rep := env.Data
	if rep.Launcher != "prism" || rep.Instance != "shulker-my-pack" || rep.Mode != "sync" || rep.Created || rep.GameDir != filepath.Join(instDir, "minecraft") || rep.Command != wantCmd {
		t.Fatalf("json report: %+v", rep)
	}
	if info, err := os.Lstat(rep.GameDir); err != nil || !info.IsDir() {
		t.Fatalf("switching back to sync mode must restore a real game directory: %v %v", info, err)
	}
}

func TestLinkPrismTargetNameAndErrors(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	path := filepath.Join(h.dir, manifest.FileName)
	data, _ := os.ReadFile(path)
	data = []byte(strings.Replace(string(data), `"side": "client"`, `"name": "Pack (dev)",
      "side": "client"`, 1))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	launcherDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(launcherDir, "prismlauncher.cfg"), []byte("[General]\nInstanceDir=custom\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "link", "prism", "--launcher-dir", launcherDir)
	cfg := readINIFile(t, filepath.Join(launcherDir, "custom", "shulker-pack-dev", launcher.InstanceConfigFile))
	if cfg["name"] != "Pack (dev)" {
		t.Fatalf("instance.cfg: %v", cfg)
	}
	h.mustRun(t, "link", "mojang", "--launcher-dir", launcherDir)
	var profiles struct {
		Profiles map[string]struct {
			Name string `json:"name"`
		} `json:"profiles"`
	}
	readJSONFile(t, filepath.Join(launcherDir, launcher.ProfilesFile), &profiles)
	if profiles.Profiles["shulker-pack-dev"].Name != "Pack (dev)" {
		t.Fatalf("mojang profiles: %+v", profiles.Profiles)
	}

	code, stdout, _ := h.run(t, "link", "multimc", "--json")
	if code == 0 || failureCode(t, stdout).Code != "launcher-dir-required" {
		t.Fatalf("multimc without dir: exit %d %s", code, stdout)
	}
	code, stdout, _ = h.run(t, "link", "prism", "--launcher-dir", filepath.Join(t.TempDir(), "missing"), "--json")
	if code == 0 || failureCode(t, stdout).Code != "launcher-not-found" {
		t.Fatalf("missing launcher: exit %d %s", code, stdout)
	}
	code, stdout, _ = h.run(t, "link", "prism", "--launcher-dir", launcherDir, "--mode", "hardlink", "--json")
	if code == 0 || failureCode(t, stdout).Code != "usage" {
		t.Fatalf("bad mode: exit %d %s", code, stdout)
	}
	gameDir := filepath.Join(launcherDir, "custom", "shulker-pack-dev", "minecraft")
	if err := os.WriteFile(filepath.Join(gameDir, "options.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ = h.run(t, "link", "prism", "--launcher-dir", launcherDir, "--mode", "symlink", "--json")
	if code == 0 || failureCode(t, stdout).Code != "instance-dir-not-empty" {
		t.Fatalf("non-empty game dir: exit %d %s", code, stdout)
	}
}

func readINIFile(t *testing.T, path string) map[string]string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(data), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if strings.HasPrefix(value, `"`) && strings.HasSuffix(value, `"`) {
			value = strings.NewReplacer(`\\`, `\`, `\"`, `"`).Replace(value[1 : len(value)-1])
		}
		values[key] = value
	}
	return values
}

func TestLinkPrismConfigFormats(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "my-pack")
	h.mustRun(t, "add", "sodium")
	cmdValue := `sh "$INST_MC_DIR/.shulker/pre-launch"`

	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir)
	prismCfg := filepath.Join(prismDir, "instances", "shulker-my-pack", launcher.InstanceConfigFile)
	lines := rawINILines(t, prismCfg)
	quoted := `PreLaunchCommand="` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(cmdValue) + `"`
	if !lines["ConfigVersion=1.3"] || !lines[quoted] {
		t.Fatalf("prism instance.cfg:\n%s", readFile(t, prismCfg))
	}
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir)
	if strings.Count(readFile(t, prismCfg), "ConfigVersion=") != 1 {
		t.Fatalf("ConfigVersion duplicated:\n%s", readFile(t, prismCfg))
	}

	multimcDir := t.TempDir()
	h.mustRun(t, "link", "multimc", "--launcher-dir", multimcDir)
	multimcCfg := filepath.Join(multimcDir, "instances", "shulker-my-pack", launcher.InstanceConfigFile)
	content := readFile(t, multimcCfg)
	if strings.Contains(content, "ConfigVersion") || !rawINILines(t, multimcCfg)["PreLaunchCommand="+cmdValue] {
		t.Fatalf("multimc instance.cfg:\n%s", content)
	}
}

func rawINILines(t *testing.T, path string) map[string]bool {
	t.Helper()
	lines := map[string]bool{}
	for _, line := range strings.Split(readFile(t, path), "\n") {
		lines[line] = true
	}
	return lines
}
