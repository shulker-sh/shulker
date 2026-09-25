package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/saves"
)

func backupOf(t *testing.T, h *harness, args ...string) backupResult {
	t.Helper()
	var env struct {
		Data backupResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, append([]string{"backup", "--json"}, args...)...)), &env); err != nil {
		t.Fatal(err)
	}
	return env.Data
}

func TestBackupZipsAnInstancesSaveGroup(t *testing.T) {
	h := newHarness(t)
	root := savesRoot(t, h)
	linkShulkerPack(t, h)
	addWorld(t, filepath.Join(root, "default"), "survival")
	addWorld(t, filepath.Join(root, "default"), "creative")
	data, err := config.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Join(data, "backups")) })

	stdout, stderr := h.mustRunStderr(t, "backup")
	if !strings.Contains(stderr, "zipped creative") || !strings.Contains(stderr, "zipped survival") {
		t.Fatalf("a step per world: %s", stderr)
	}
	if !regexp.MustCompile(`backed up 2 worlds » .*/backups/default/\d{8}-\d{6}-pack-backup\.zip \(.* in \d+\.\ds\)`).MatchString(stdout) {
		t.Fatalf("backup: %s", stdout)
	}

	project := h.dir
	h.dir = ""
	stdout = h.mustRun(t, "saves", "-i", "pack")
	h.dir = project
	rows := tableRows(stdout)
	if len(rows) != 1 || !strings.HasSuffix(rows[0]["Backup"], "-pack-backup") || rows[0]["Reason"] != "on request" || rows[0]["Worlds"] != "2" || rows[0]["Game"] != "26.2 fabric 0.17.3" {
		t.Fatalf("saves -i pack: %s", stdout)
	}

	h.dir = ""
	if got := backupOf(t, h, "-i", "pack"); got.Group != "default" || !strings.HasSuffix(got.ID, "-pack-backup") || got.Minecraft != "26.2" {
		t.Fatalf("backup -i pack: %+v", got)
	}
	if got := backupOf(t, h, "--group", "default"); got.Group != "default" || filepath.Dir(got.Path) != filepath.Join(data, "backups", "default") || !strings.HasSuffix(got.ID, "-backup") || strings.Contains(got.ID, "pack") {
		t.Fatalf("a group named alone has no instance to name: %+v", got)
	}
	if env := h.runSetting(t, 2, "backup", "--group", "default", "-i", "pack"); env.Error == nil || env.Error.Code != "usage" {
		t.Fatalf("--group with -i: %+v", env.Error)
	}
	if env := h.runSetting(t, 1, "backup", "--group", "missing"); env.Error == nil || env.Error.Code != "group-not-found" {
		t.Fatalf("an unknown group: %+v", env.Error)
	}
}

func TestBackupKeepsAnInstancesOwnWorldsInItsFolder(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	gameDir := filepath.Join(t.TempDir(), "prism", "pack", "minecraft")
	h.mustRun(t, "sync", h.dir, "--into", gameDir)

	if env := h.runSetting(t, 1, "backup", "-C", gameDir); env.Error == nil || env.Error.Code != "no-worlds" || !strings.Contains(env.Error.Message, filepath.Join(gameDir, "saves")) {
		t.Fatalf("no worlds: %+v", env.Error)
	}
	addWorld(t, filepath.Join(gameDir, "saves"), "mine")
	if stdout := h.mustRun(t, "saves", "-C", gameDir); !strings.Contains(stdout, "no backups yet; shulker backup -C "+gameDir+" takes one") {
		t.Fatalf("the hint names the backup for this target: %s", stdout)
	}
	got := backupOf(t, h, "-C", gameDir)
	if filepath.Dir(got.Path) != filepath.Join(gameDir, instance.Dir, "backups") || !regexp.MustCompile(`^\d{8}-\d{6}-backup\.zip$`).MatchString(filepath.Base(got.Path)) {
		t.Fatalf("backup went to %s", got.Path)
	}
	if got.Worlds != 1 || strings.Join(got.Names, ",") != "mine" || got.Minecraft != "26.2" || got.Loader != "fabric" || got.LoaderVersion != "0.17.3" {
		t.Fatalf("backed up %+v", got)
	}
}

func TestBackupOfAServerTakesOnlyItsLevel(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack", "--side", "server")
	h.mustRun(t, "install")
	buildDir := filepath.Join(h.dir, "build", "server")
	if env := h.runSetting(t, 1, "backup", "-C", buildDir); env.Error == nil || env.Error.Code != "no-worlds" || !strings.Contains(env.Error.Message, "no world world in") {
		t.Fatalf("no level yet: %+v", env.Error)
	}
	worlds := filepath.Join(h.dir, "data", "server")
	addWorld(t, worlds, "world")
	addWorld(t, worlds, "old-world")
	if got := backupOf(t, h, "-C", buildDir); strings.Join(got.Names, ",") != "world" {
		t.Fatalf("backed up %v", got.Names)
	}
}

func TestBackupOfAStateWithoutAPlatformNamesNone(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	gameDir := filepath.Join(t.TempDir(), "game")
	h.mustRun(t, "sync", h.dir, "--into", gameDir)
	addWorld(t, filepath.Join(gameDir, "saves"), "mine")
	state := build.LoadState(gameDir)
	state.Minecraft, state.Loader, state.LoaderVersion = "", "", ""
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(build.StatePath(gameDir), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := backupOf(t, h, "-C", gameDir); got.Minecraft != "" || got.Loader != "" || got.LoaderVersion != "" {
		t.Fatalf("backed up %+v", got)
	}
	if rows := tableRows(h.mustRun(t, "saves", "-C", gameDir)); len(rows) != 1 || rows[0]["Game"] != "" {
		t.Fatalf("saves: %+v", rows)
	}
}

func TestBackupGameNamesTheLoaderWithoutAVersion(t *testing.T) {
	b := saves.Backup{Reason: "sync", Worlds: 1, Minecraft: "26.2", Loader: "fabric"}
	if got := backupGame(b); got != "26.2 fabric" {
		t.Fatalf("game: %s", got)
	}
	if got := backupReason(b); got != "before sync" {
		t.Fatalf("reason: %s", got)
	}
}

func TestBackupOnlyTheWorldsNamed(t *testing.T) {
	h := newHarness(t)
	root := savesRoot(t, h)
	linkShulkerPack(t, h)
	group := filepath.Join(root, "default")
	for _, w := range []string{"a", "b", "c"} {
		addWorld(t, group, w)
	}
	data, err := config.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Join(data, "backups")) })

	got := backupOf(t, h, "--world", "a", "--world", "b")
	if got.Worlds != 2 || strings.Join(got.Names, ",") != "a,b" {
		t.Fatalf("backup --world a --world b: %+v", got)
	}
	if env := h.runSetting(t, 1, "backup", "--world", "a", "--world", "nope"); env.Error == nil || env.Error.Code != "world-not-found" || !strings.Contains(env.Error.Message, "nope") {
		t.Fatalf("a world the target doesn't hold: %+v", env.Error)
	}
}

func TestBackupAServerHoldsOnlyItsLevel(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack", "--side", "server")
	h.mustRun(t, "install")
	worlds := filepath.Join(h.dir, "data", "server")
	addWorld(t, worlds, "world")
	addWorld(t, worlds, "old-world")
	buildDir := filepath.Join(h.dir, "build", "server")

	if got := backupOf(t, h, "-C", buildDir, "--world", "world"); got.Worlds != 1 {
		t.Fatalf("the level by name: %+v", got)
	}
	if env := h.runSetting(t, 1, "backup", "-C", buildDir, "--world", "old-world"); env.Error == nil || env.Error.Code != "world-not-found" || !strings.Contains(env.Error.Message, "loads only world") {
		t.Fatalf("a world beside the level: %+v", env.Error)
	}
}
