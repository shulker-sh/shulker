package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/saves/savestest"
)

func restoreOf(t *testing.T, h *harness, args ...string) restoreResult {
	t.Helper()
	var env struct {
		Data restoreResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, append([]string{"restore", "--json"}, args...)...)), &env); err != nil {
		t.Fatal(err)
	}
	return env.Data
}

func TestRestorePutsTheZipsWorldsBackWhole(t *testing.T) {
	h := newHarness(t)
	root := savesRoot(t, h)
	linkShulkerPack(t, h)
	group := filepath.Join(root, "default")
	addWorld(t, group, "survival")
	addWorld(t, group, "hardcore")
	data, err := config.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Join(data, "backups")) })

	if env := h.runSetting(t, 1, "restore"); env.Error == nil || env.Error.Code != "backups-empty" {
		t.Fatalf("no backups: %+v", env.Error)
	}
	first := backupOf(t, h)
	if err := os.WriteFile(filepath.Join(group, "survival", "since.mca"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	addWorld(t, group, "creative")
	if err := os.RemoveAll(filepath.Join(group, "hardcore")); err != nil {
		t.Fatal(err)
	}

	stdout, stderr := h.mustRunStderr(t, "restore")
	if !strings.Contains(stderr, "unzipped survival") {
		t.Fatalf("a step per world: %s", stderr)
	}
	for _, want := range []string{"-pack-restore", "restored 2 worlds from " + first.ID + " » " + group, "~ survival", "+ hardcore"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("restore lacks %q: %s", want, stdout)
		}
	}
	if _, err := os.Stat(filepath.Join(group, "survival", "since.mca")); err == nil {
		t.Fatal("a replaced world kept a file from after the backup")
	}
	if _, err := os.Stat(filepath.Join(group, "creative", "level.dat")); err != nil {
		t.Fatal("a world the zip doesn't hold was touched")
	}

	project := h.dir
	h.dir = ""
	stdout = h.mustRun(t, "saves", "-i", "pack")
	h.dir = project
	for _, want := range []string{"Restore one:", "shulker -i pack restore <n>"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("saves lacks %q: %s", want, stdout)
		}
	}
	rows := tableRows(stdout)
	if len(rows) != 2 || rows[0]["#"] != "1" || !strings.HasSuffix(rows[0]["Backup"], "-pack-restore") || rows[0]["Reason"] != "before a restore" || rows[0]["Worlds"] != "2" || rows[1]["#"] != "2" || rows[1]["Backup"] != first.ID {
		t.Fatalf("saves: %s", stdout)
	}

	if got := restoreOf(t, h, "2"); got.From.ID != first.ID {
		t.Fatalf("restore 2 picked %s", got.From.ID)
	}
	if got := restoreOf(t, h, "--backup", first.ID); got.From.ID != first.ID || got.From.Reason != "backup" || got.Snapshot == nil || got.Snapshot.Reason != "restore" {
		t.Fatalf("--backup by name: %+v", got)
	}
	h.dir = ""
	if got := restoreOf(t, h, "-i", "pack", "--backup", first.Path); got.From.Path != first.Path || got.WorldsDir != group {
		t.Fatalf("-i with --backup by path: %+v", got)
	}
	h.dir = project
	if env := h.runSetting(t, 1, "restore", "99"); env.Error == nil || env.Error.Code != "backup-missing" {
		t.Fatalf("past the end: %+v", env.Error)
	}
	if env := h.runSetting(t, 1, "restore", "--backup", "nope"); env.Error == nil || env.Error.Code != "backup-missing" {
		t.Fatalf("an unknown name: %+v", env.Error)
	}
	if env := h.runSetting(t, 2, "restore", "1", "--backup", first.ID); env.Error == nil || env.Error.Code != "usage" {
		t.Fatalf("an index and --backup: %+v", env.Error)
	}
}

func TestRestoreAZipFromElsewhereIntoAnyTarget(t *testing.T) {
	h := newHarness(t)
	root := savesRoot(t, h)
	h.mustRun(t, "create", "--loader", "fabric")
	gameDir := filepath.Join(t.TempDir(), "prism", "pack", "minecraft")
	h.mustRun(t, "sync", h.dir, "--into", gameDir)
	addWorld(t, filepath.Join(gameDir, "saves"), "mine")
	zipped := backupOf(t, h, "-C", gameDir)
	moved := filepath.Join(t.TempDir(), "anything.zip")
	if err := os.Rename(zipped.Path, moved); err != nil {
		t.Fatal(err)
	}

	group := filepath.Join(root, "shared")
	if err := os.MkdirAll(group, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := config.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Join(data, "backups")) })
	got := restoreOf(t, h, "--group", "shared", "--backup", moved)
	if got.Snapshot != nil || len(got.Worlds) != 1 || got.Worlds[0].Name != "mine" || got.Worlds[0].Replaced {
		t.Fatalf("into a group: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(group, "mine", "level.dat")); err != nil {
		t.Fatal(err)
	}
	got = restoreOf(t, h, "--group", "shared", "--backup", moved)
	if got.Snapshot == nil || filepath.Base(got.Snapshot.Path) != got.Snapshot.ID+".zip" || !strings.HasSuffix(got.Snapshot.ID, "-restore") || strings.Count(got.Snapshot.ID, "-") != 2 {
		t.Fatalf("a group's own pre-restore backup names no instance: %+v", got.Snapshot)
	}

	other := filepath.Join(t.TempDir(), "other", "minecraft")
	h.mustRun(t, "sync", h.dir, "--into", other)
	if env := h.runSetting(t, 1, "restore", "-C", other); env.Error == nil || env.Error.Code != "backups-empty" {
		t.Fatalf("no backups: %+v", env.Error)
	}
	if got := restoreOf(t, h, "-C", other, "--backup", moved); got.WorldsDir != filepath.Join(other, "saves") || len(got.Worlds) != 1 {
		t.Fatalf("into another directory: %+v", got)
	}

	notZip := filepath.Join(t.TempDir(), "notes.zip")
	if err := os.WriteFile(notZip, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	if env := h.runSetting(t, 1, "restore", "-C", other, "--backup", notZip); env.Error == nil || env.Error.Code != "backup-invalid" {
		t.Fatalf("not a zip: %+v", env.Error)
	}
}

func TestRestoreIntoAServerTakesOnlyItsLevel(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack", "--side", "server")
	h.mustRun(t, "install")
	buildDir := filepath.Join(h.dir, "build", "server")
	elsewhere := t.TempDir()
	addWorld(t, filepath.Join(elsewhere, "saves"), "world")
	addWorld(t, filepath.Join(elsewhere, "saves"), "old-world")
	zipped := backupOf(t, h, "-C", elsewhere)

	only := filepath.Join(t.TempDir(), "only-old.zip")
	onlyWorlds := t.TempDir()
	addWorld(t, filepath.Join(onlyWorlds, "saves"), "old-world")
	if err := os.Rename(backupOf(t, h, "-C", onlyWorlds).Path, only); err != nil {
		t.Fatal(err)
	}
	if env := h.runSetting(t, 1, "restore", "-C", buildDir, "--backup", only); env.Error == nil || env.Error.Code != "world-not-found" || !strings.Contains(env.Error.Message, "no world world") {
		t.Fatalf("no level in the zip: %+v", env.Error)
	}
	got := restoreOf(t, h, "-C", buildDir, "--backup", zipped.Path)
	if len(got.Worlds) != 1 || got.Worlds[0].Name != "world" {
		t.Fatalf("restored %+v", got.Worlds)
	}
	worlds := filepath.Join(h.dir, "data", "server")
	if _, err := os.Stat(filepath.Join(worlds, "old-world")); err == nil {
		t.Fatal("restored a world the server doesn't load")
	}
}

func TestAWorldOpenInARunningGame(t *testing.T) {
	h := newHarness(t)
	root := savesRoot(t, h)
	linkShulkerPack(t, h)
	group := filepath.Join(root, "default")
	addWorld(t, group, "survival")
	addWorld(t, group, "creative")
	data, err := config.DataDir()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(filepath.Join(data, "backups")) })
	first := backupOf(t, h)
	if err := os.WriteFile(filepath.Join(group, "survival", "since.mca"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	savestest.Hold(t, filepath.Join(group, "survival"))

	stdout, stderr := h.mustRunStderr(t, "backup")
	if !strings.Contains(stderr, "survival is open in a running game; its backup may be torn") || strings.Contains(stderr, "creative is open") {
		t.Fatalf("backup warns about only the open world: %s", stderr)
	}
	if !strings.Contains(stdout, "backed up 2 worlds") {
		t.Fatalf("backup zips anyway: %s", stdout)
	}

	env := h.runSetting(t, 1, "restore", "--backup", first.ID)
	if env.Error == nil || env.Error.Code != "world-in-use" || strings.Join(env.Error.Items, ",") != "survival" {
		t.Fatalf("restore over an open world: %+v", env.Error)
	}
	if _, err := os.Stat(filepath.Join(group, "survival", "since.mca")); err != nil {
		t.Fatal("a refused restore changed the world")
	}
	if backups, _ := os.ReadDir(filepath.Join(data, "backups", "default")); len(backups) != 2 {
		t.Fatalf("a refused restore took a backup: %d", len(backups))
	}
}

func TestRestoreOnlyTheWorldsNamed(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	gameDir := filepath.Join(t.TempDir(), "minecraft")
	h.mustRun(t, "sync", h.dir, "--into", gameDir)
	worlds := filepath.Join(gameDir, "saves")
	addWorld(t, worlds, "a")
	addWorld(t, worlds, "b")
	zipped := backupOf(t, h, "-C", gameDir)
	for _, w := range []string{"a", "b"} {
		if err := os.WriteFile(filepath.Join(worlds, w, "since.mca"), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	got := restoreOf(t, h, "-C", gameDir, "--backup", zipped.Path, "--world", "a")
	if len(got.Worlds) != 1 || got.Worlds[0].Name != "a" || got.Snapshot == nil {
		t.Fatalf("restore --world a: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(worlds, "a", "since.mca")); err == nil {
		t.Fatal("a was not restored")
	}
	if _, err := os.Stat(filepath.Join(worlds, "b", "since.mca")); err != nil {
		t.Fatal("b was touched")
	}
	if env := h.runSetting(t, 1, "restore", "-C", gameDir, "--backup", zipped.Path, "--world", "nope"); env.Error == nil || env.Error.Code != "world-not-found" || !strings.Contains(env.Error.Message, "nope") {
		t.Fatalf("a world the zip doesn't hold: %+v", env.Error)
	}

	if env := h.runSetting(t, 2, "restore", "-C", gameDir, "--backup", zipped.Path, "--as", "c"); env.Error == nil || env.Error.Code != "usage" {
		t.Fatalf("--as with two worlds: %+v", env.Error)
	}
	for _, bad := range []string{".", "..", "x/y", "/tmp/z"} {
		if env := h.runSetting(t, 2, "restore", "-C", gameDir, "--backup", zipped.Path, "--world", "b", "--as", bad); env.Error == nil || env.Error.Code != "usage" {
			t.Fatalf("--as %s: %+v", bad, env.Error)
		}
	}
	if _, err := os.Stat(filepath.Join(worlds, "a", "level.dat")); err != nil {
		t.Fatal("a refused --as touched the worlds")
	}
	addWorld(t, worlds, "held")
	savestest.Hold(t, filepath.Join(worlds, "held"))
	if env := h.runSetting(t, 1, "restore", "-C", gameDir, "--backup", zipped.Path, "--world", "b", "--as", "held"); env.Error == nil || env.Error.Code != "world-in-use" || strings.Join(env.Error.Items, ",") != "held" {
		t.Fatalf("--as over an open world: %+v", env.Error)
	}
	stdout := h.mustRun(t, "restore", "-C", gameDir, "--backup", zipped.Path, "--world", "b", "--as", "c")
	if !strings.Contains(stdout, "+ c (from b)") {
		t.Fatalf("a renamed world: %s", stdout)
	}
	if _, err := os.Stat(filepath.Join(worlds, "c", "level.dat")); err != nil {
		t.Fatal("--as didn't land the world under its new name")
	}
	if _, err := os.Stat(filepath.Join(worlds, "b", "since.mca")); err != nil {
		t.Fatal("--as touched the world under its old name")
	}
}

func TestRestoreIntoAServerWithAnotherLevel(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack", "--side", "server")
	h.mustRun(t, "install")
	buildDir := filepath.Join(h.dir, "build", "server")
	other := t.TempDir()
	addWorld(t, other, "smp")
	if err := os.WriteFile(filepath.Join(other, "server.properties"), []byte("level-name=smp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	zipped := backupOf(t, h, "-C", other)

	if env := h.runSetting(t, 1, "restore", "-C", buildDir, "--backup", zipped.Path); env.Error == nil || env.Error.Code != "world-not-found" || !strings.Contains(env.Error.Message, "no world world") {
		t.Fatalf("another server's level: %+v", env.Error)
	}
	if env := h.runSetting(t, 2, "restore", "-C", buildDir, "--backup", zipped.Path, "--as", "elsewhere"); env.Error == nil || env.Error.Code != "usage" || !strings.Contains(env.Error.Message, "world") {
		t.Fatalf("--as other than the level: %+v", env.Error)
	}
	got := restoreOf(t, h, "-C", buildDir, "--backup", zipped.Path, "--as", "world")
	if len(got.Worlds) != 1 || got.Worlds[0].Name != "world" || got.Worlds[0].From != "smp" {
		t.Fatalf("restore --as world: %+v", got.Worlds)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "data", "server", "world", "level.dat")); err != nil {
		t.Fatal(err)
	}
}
