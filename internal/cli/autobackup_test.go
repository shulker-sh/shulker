package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/saves"
)

func backupReasons(t *testing.T, dir string) []string {
	t.Helper()
	backups, err := saves.Backups(dir)
	if err != nil {
		t.Fatal(err)
	}
	reasons := []string{}
	for _, b := range backups {
		reasons = append(reasons, b.Reason)
	}
	return reasons
}

func TestSyncBacksUpWorldsBeforeTheModsChange(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	gameDir := filepath.Join(t.TempDir(), "game")
	h.mustRun(t, "sync", h.dir, "--into", gameDir)
	addWorld(t, filepath.Join(gameDir, "saves"), "mine")
	home := filepath.Join(gameDir, instance.Dir, "backups")

	h.mustRun(t, "sync")
	if got := backupReasons(t, home); len(got) != 0 {
		t.Fatalf("a sync that changes no mod takes no backup: %v", got)
	}
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "sync")
	if got := backupReasons(t, home); !slices.Equal(got, []string{"sync"}) {
		t.Fatalf("a sync that adds a mod backs up first: %v", got)
	}
	if b, _ := saves.Backups(home); b[0].Minecraft != "26.2" || b[0].Loader != "fabric" || b[0].LoaderVersion != "0.17.3" {
		t.Fatalf("the backup names the platform the directory was built with: %+v", b[0])
	}
	h.mustRun(t, "sync")
	if got := backupReasons(t, home); len(got) != 1 {
		t.Fatalf("the next sync changes nothing: %v", got)
	}
}

func TestUpdateBacksUpAnInstancesWorlds(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "install")
	addWorld(t, filepath.Join(h.dir, "saves"), "mine")
	h.newer = true
	h.mustRun(t, "update")
	if got := backupReasons(t, filepath.Join(h.dir, instance.Dir, "backups")); !slices.Equal(got, []string{"update"}) {
		t.Fatalf("update backs up the instance it changes: %v", got)
	}
}

func TestUpdateBacksUpUnderThePlatformTheWorldsWerePlayedOn(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "install")
	addWorld(t, filepath.Join(h.dir, "saves"), "mine")
	state := build.LoadState(h.dir)
	state.Minecraft, state.LoaderVersion = "26.1", "0.17.2"
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(build.StatePath(h.dir), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	h.newer = true
	h.mustRun(t, "update")
	backups, err := saves.Backups(filepath.Join(h.dir, instance.Dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 1 || backups[0].Minecraft != "26.1" || backups[0].Loader != "fabric" || backups[0].LoaderVersion != "0.17.2" {
		t.Fatalf("the backup names the platform the worlds were played on: %+v", backups)
	}
	if rows := tableRows(h.mustRun(t, "saves", "-C", h.dir)); len(rows) != 1 || rows[0]["Game"] != "26.1 fabric 0.17.2" {
		t.Fatalf("saves: %+v", rows)
	}
	if got := build.LoadState(h.dir); got.Minecraft != "26.2" || got.Loader != "fabric" || got.LoaderVersion != "0.17.3" {
		t.Fatalf("the build records what it installed: %+v", got)
	}
}

func TestAutomaticBackupsWithoutWorlds(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "sync")
	if _, err := os.Stat(filepath.Join(h.dir, instance.Dir, "backups")); !os.IsNotExist(err) {
		t.Fatalf("a worldless instance backs up as nothing: %v", err)
	}
}

func TestAutomaticBackupsTrimToSaveBackups(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "install")
	addWorld(t, filepath.Join(h.dir, "saves"), "mine")
	home := filepath.Join(h.dir, instance.Dir, "backups")
	if err := os.MkdirAll(home, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"20260910-000000-backup.zip", "20260911-000000-sync.zip", "20260912-000000-restore.zip", "20260913-000000-update.zip"} {
		if err := os.WriteFile(filepath.Join(home, name), []byte("zip"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h.mustRun(t, "config", "set", "play.saveBackups", "2")
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "sync")
	if got := backupReasons(t, home); !slices.Equal(got, []string{"sync", "update", "restore", "backup"}) {
		t.Fatalf("the oldest automatic backup goes and the kept ones stay: %v", got)
	}

	h.mustRun(t, "config", "set", "play.saveBackups", "0")
	h.mustRun(t, "remove", "sodium")
	h.mustRun(t, "sync")
	if got := backupReasons(t, home); len(got) != 4 {
		t.Fatalf("0 takes no automatic backup: %v", got)
	}
}

func TestAutomaticBackupThatCantBeWrittenStopsTheSync(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "install")
	addWorld(t, filepath.Join(h.dir, "saves"), "mine")
	if err := os.WriteFile(filepath.Join(h.dir, instance.Dir, "backups"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "add", "sodium")
	if code, _, _ := h.run(t, "sync"); code == 0 {
		t.Fatal("a backup that can't be written must stop the sync")
	}
	if _, err := os.Stat(filepath.Join(h.dir, "mods", h.jars["sodium"].filename)); !os.IsNotExist(err) {
		t.Fatalf("no mod changes without the backup: %v", err)
	}
}

func TestAutomaticBackupWarnsWhenConfigIsUnreadable(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	gameDir := filepath.Join(t.TempDir(), "game")
	h.mustRun(t, "sync", h.dir, "--into", gameDir)
	addWorld(t, filepath.Join(gameDir, "saves"), "mine")
	h.mustRun(t, "add", "sodium")
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	h.config = filepath.Join(blocker, "config.json")
	_, stderr := h.mustRunStderr(t, "sync", h.dir, "--into", gameDir)
	if !strings.Contains(stderr, "couldn't back up the worlds in "+gameDir+" before the mods changed") {
		t.Fatalf("stderr: %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("the sync carries on: %v", err)
	}
}
