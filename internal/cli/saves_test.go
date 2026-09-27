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

func savesRoot(t *testing.T, h *harness) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "saves")
	h.mustRun(t, "config", "set", "saves", root)
	return root
}

func addWorld(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name, "level.dat"), []byte("nbt"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func savesLink(t *testing.T, gameDir string) string {
	t.Helper()
	target, err := os.Readlink(filepath.Join(gameDir, "saves"))
	if err != nil {
		t.Fatalf("saves is not a link: %v", err)
	}
	return target
}

// linkShulkerPack makes a shulker instance named pack and returns its game directory, with h.dir
// pointed at it so later commands act on the instance.
func linkShulkerPack(t *testing.T, h *harness) string {
	t.Helper()
	root := shulkerInstances(t, h)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	stdout := h.mustRun(t, "link", "shulker")
	if !strings.Contains(stdout, "✔ Synced ") || strings.Contains(stdout, "saves") {
		t.Fatalf("joining the default group with nothing to move says nothing of saves: %s", stdout)
	}
	h.dir = filepath.Join(root, "pack")
	return h.dir
}

func TestShulkerInstanceJoinsDefaultGroup(t *testing.T) {
	h := newHarness(t)
	root := savesRoot(t, h)
	gameDir := linkShulkerPack(t, h)
	if got := savesLink(t, gameDir); got != filepath.Join(root, "default") {
		t.Fatalf("saves points at %s", got)
	}
	if stdout := h.mustRun(t, "sync"); strings.Contains(stdout, "saves:") {
		t.Fatalf("an unchanged link reports nothing: %s", stdout)
	}
}

func TestSavesGroupRelinksOnSync(t *testing.T) {
	h := newHarness(t)
	root := savesRoot(t, h)
	gameDir := linkShulkerPack(t, h)
	addWorld(t, filepath.Join(root, "default"), "survival")
	addWorld(t, filepath.Join(root, "hardcore"), "one-life")

	h.mustRun(t, "instance", "set", "savesGroup", "hardcore")
	if got := savesLink(t, gameDir); got != filepath.Join(root, "default") {
		t.Fatalf("setting the group waits for the next sync: %s", got)
	}
	stdout := h.mustRun(t, "sync")
	if !strings.Contains(stdout, `Worlds from save group "hardcore"`) {
		t.Fatalf("sync reports the worlds now visible: %s", stdout)
	}
	if got := savesLink(t, gameDir); got != filepath.Join(root, "hardcore") {
		t.Fatalf("saves points at %s", got)
	}

	h.mustRun(t, "instance", "set", "savesGroup", "none")
	var env struct {
		Data syncResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "sync", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if s := env.Data.Saves; s == nil || !s.Changed || s.Group != "none" || len(s.Worlds) != 0 {
		t.Fatalf("saves = %+v", env.Data.Saves)
	}
	info, err := os.Lstat(filepath.Join(gameDir, "saves"))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("with none the instance keeps its own saves folder: %v %v", info, err)
	}
	for _, w := range []string{filepath.Join(root, "default", "survival"), filepath.Join(root, "hardcore", "one-life")} {
		if _, err := os.Stat(filepath.Join(w, "level.dat")); err != nil {
			t.Fatalf("leaving a group leaves its worlds: %v", err)
		}
	}
}

func TestSavesGroupRefusesABadName(t *testing.T) {
	h := newHarness(t)
	linkShulkerPack(t, h)
	if env := h.runSetting(t, 1, "instance", "set", "savesGroup", "../elsewhere"); env.Error == nil {
		t.Fatal("a group is a single folder name")
	}
	h.mustRun(t, "sync")
}

func TestOtherLaunchersKeepTheirSaves(t *testing.T) {
	h := newHarness(t)
	savesRoot(t, h)
	h.mustRun(t, "create", "--loader", "fabric")
	gameDir := filepath.Join(t.TempDir(), "game")
	h.mustRun(t, "sync", h.dir, "--into", gameDir)
	if _, err := os.Readlink(filepath.Join(gameDir, "saves")); err == nil {
		t.Fatal("only shulker's own instances join a save group")
	}
}

func TestSavesListsGroupsWorldsAndBackups(t *testing.T) {
	h := newHarness(t)
	root := savesRoot(t, h)
	linkShulkerPack(t, h)
	addWorld(t, filepath.Join(root, "default"), "survival")
	addWorld(t, filepath.Join(root, "default"), "creative")
	if err := os.MkdirAll(filepath.Join(root, "hardcore"), 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := config.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	backups := filepath.Join(data, "backups", "default")
	if err := os.MkdirAll(backups, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Join(data, "backups")) })
	for _, name := range []string{"20260917-101500-pack-sync.zip", "20260918-203015-pack-update.zip", "20260918-210000-pack-backup.zip"} {
		if err := os.WriteFile(filepath.Join(backups, name), []byte("zip"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	stdout := h.mustRun(t, "saves")
	if !strings.Contains(stdout, "default (2 worlds") || !strings.Contains(stdout, "last backup 2026-09-18 21:00") || !strings.Contains(stdout, "hardcore (no worlds") {
		t.Fatalf("saves: %s", stdout)
	}

	project := h.dir
	h.dir = ""
	stdout = h.mustRun(t, "saves", "-i", "pack")
	h.dir = project
	for _, want := range []string{"Worlds", "creative", "survival", "Backups"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("saves -i pack lacks %q: %s", want, stdout)
		}
	}
	rows := tableRows(stdout)
	if len(rows) != 3 || rows[0]["#"] != "1" || rows[0]["Backup"] != "20260918-210000-pack-backup" || rows[0]["Reason"] != "on request" || rows[2]["#"] != "3" || rows[2]["Backup"] != "20260917-101500-pack-sync" || rows[2]["Reason"] != "before sync" {
		t.Fatalf("saves -i pack: %s", stdout)
	}
	if stdout := h.mustRun(t, "saves", "--group", "hardcore"); !strings.Contains(stdout, "No worlds") || !strings.Contains(stdout, "shulker backup --group hardcore") {
		t.Fatalf("saves --group: %s", stdout)
	}
	if env := h.runSetting(t, 1, "saves", "--group", "missing"); env.Error == nil || env.Error.Code != "group-not-found" {
		t.Fatalf("an unknown group: %+v", env.Error)
	}

	if env := h.runSetting(t, 2, "saves", "prune", "--group", "default"); env.Error == nil || env.Error.Code != "usage" {
		t.Fatalf("prune needs --keep: %+v", env.Error)
	}
	stdout = h.mustRun(t, "saves", "prune", "--group", "default", "--keep", "1")
	if !strings.Contains(stdout, "- 20260918-203015-pack-update") || !strings.Contains(stdout, "- 20260917-101500-pack-sync") || !strings.Contains(stdout, "Pruned 2 backups") {
		t.Fatalf("prune: %s", stdout)
	}
	left, _ := os.ReadDir(backups)
	if len(left) != 1 || left[0].Name() != "20260918-210000-pack-backup.zip" {
		t.Fatalf("left = %v", left)
	}
}

func TestSavesTargetsAnInstanceWithoutAGroup(t *testing.T) {
	h := newHarness(t)
	gameDir := linkShulkerPack(t, h)
	h.mustRun(t, "instance", "set", "savesGroup", "none")
	h.mustRun(t, "sync")
	addWorld(t, filepath.Join(gameDir, "saves"), "mine")
	own := filepath.Join(gameDir, instance.Dir, "backups")
	if err := os.MkdirAll(own, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(own, "20260918-203015-update.zip"), []byte("zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout := h.mustRun(t, "saves", "-C", gameDir)
	if rows := tableRows(stdout); !strings.Contains(stdout, "mine") || len(rows) != 1 || rows[0]["#"] != "1" || rows[0]["Backup"] != "20260918-203015-update" || rows[0]["Reason"] != "before update" {
		t.Fatalf("saves -C: %s", stdout)
	}
}

func savesOf(t *testing.T, h *harness, args ...string) savesView {
	t.Helper()
	var env struct {
		Data savesView `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, append(args, "--json")...)), &env); err != nil {
		t.Fatal(err)
	}
	return env.Data
}

func wantWorlds(t *testing.T, v savesView, dir string, worlds ...string) {
	t.Helper()
	if v.WorldsDir != dir || strings.Join(v.Worlds, ",") != strings.Join(worlds, ",") {
		t.Fatalf("worlds = %v in %s, want %v in %s", v.Worlds, v.WorldsDir, worlds, dir)
	}
}

func TestSavesReadsASeparateDirBuildFromData(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "install")
	data := filepath.Join(h.dir, "data", "client", "saves")
	addWorld(t, data, "First")
	buildDir := filepath.Join(h.dir, "build", "client")

	wantWorlds(t, savesOf(t, h, "saves", "-C", buildDir), data, "First")
	var env struct {
		Data savesPruned `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "saves", "prune", "-C", buildDir, "--keep", "0", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.WorldsDir != data {
		t.Fatalf("prune resolves %s, not %s", env.Data.WorldsDir, data)
	}
}

func TestSavesReadsAnotherLaunchersInstanceInPlace(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	gameDir := filepath.Join(t.TempDir(), "game")
	h.mustRun(t, "sync", h.dir, "--into", gameDir)
	addWorld(t, filepath.Join(gameDir, "saves"), "mine")
	wantWorlds(t, savesOf(t, h, "saves", "-C", gameDir), filepath.Join(gameDir, "saves"), "mine")
}

func TestSavesReadsOnlyAServersLevelName(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack", "--side", "server")
	h.editManifest(t, func(m map[string]any) {
		m["server"].(map[string]any)["properties"].(map[string]any)["level-name"] = "creative"
	})
	h.mustRun(t, "install")
	data := filepath.Join(h.dir, "data", "server")
	addWorld(t, data, "creative")
	addWorld(t, data, "world")
	v := savesOf(t, h, "saves", "-C", filepath.Join(h.dir, "build", "server"))
	wantWorlds(t, v, data, "creative")
	if v.World != "creative" {
		t.Fatalf("world = %q", v.World)
	}

	bare := t.TempDir()
	if err := os.WriteFile(filepath.Join(bare, "server.properties"), []byte("motd=hi\nlevel-name=hub\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	addWorld(t, bare, "hub")
	addWorld(t, bare, "world")
	wantWorlds(t, savesOf(t, h, "saves", "-C", bare), bare, "hub")
	if err := os.WriteFile(filepath.Join(bare, "server.properties"), []byte("motd=hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantWorlds(t, savesOf(t, h, "saves", "-C", bare), bare, "world")
}

func TestSavesReadsAnInPlaceServersLevelNameFromItsManifest(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack", "--side", "server")
	h.editManifest(t, func(m map[string]any) {
		server := m["server"].(map[string]any)
		server["build"] = "."
		server["properties"].(map[string]any)["level-name"] = "creative"
	})
	addWorld(t, h.dir, "creative")
	addWorld(t, h.dir, "world")
	if err := os.WriteFile(filepath.Join(h.dir, "server.properties"), []byte("level-name=world\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wantWorlds(t, savesOf(t, h, "saves", "-C", h.dir), h.dir, "creative")
}
