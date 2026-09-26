package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

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
	state := instance.LoadState(h.dir)
	state.Minecraft, state.LoaderVersion = "26.1", "0.17.2"
	raw, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(instance.StatePath(h.dir), raw, 0o644); err != nil {
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
	if got := instance.LoadState(h.dir); got.Minecraft != "26.2" || got.Loader != "fabric" || got.LoaderVersion != "0.17.3" {
		t.Fatalf("the build records what it installed: %+v", got)
	}
}

func TestAutomaticBackupWarnsWhenConfigIsUnreadable(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
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
	if !strings.Contains(stderr, "Couldn't back up the worlds in "+gameDir+" before the mods changed") {
		t.Fatalf("stderr: %s", stderr)
	}
	if _, err := os.Stat(filepath.Join(gameDir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("the sync carries on: %v", err)
	}
}
