package sync

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/saves"
)

func addWorld(t *testing.T, dir, name string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, name, "level.dat"), "nbt")
}

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

func TestRunBacksUpWorldsBeforeTheModsChange(t *testing.T) {
	h := newHarness(t)
	gameDir := filepath.Join(t.TempDir(), "game")
	h.mustSync(gameDir, Request{Backup: "sync"})
	addWorld(t, filepath.Join(gameDir, "saves"), "mine")
	home := filepath.Join(gameDir, instance.Dir, "backups")

	h.mustSync(gameDir, Request{Backup: "sync"})
	if got := backupReasons(t, home); len(got) != 0 {
		t.Fatalf("a sync that changes no mod takes no backup: %v", got)
	}
	h.add("sodium")
	h.mustSync(gameDir, Request{Backup: "sync"})
	if got := backupReasons(t, home); !slices.Equal(got, []string{"sync"}) {
		t.Fatalf("a sync that adds a mod backs up first: %v", got)
	}
	if b, _ := saves.Backups(home); b[0].Minecraft != "26.2" || b[0].Loader != "fabric" || b[0].LoaderVersion != "0.17.3" {
		t.Fatalf("the backup names the platform the directory was built with: %+v", b[0])
	}
	h.mustSync(gameDir, Request{Backup: "sync"})
	if got := backupReasons(t, home); len(got) != 1 {
		t.Fatalf("the next sync changes nothing: %v", got)
	}
}

func TestRunWithoutABackupReasonTakesNone(t *testing.T) {
	h := newHarness(t)
	gameDir := filepath.Join(t.TempDir(), "game")
	h.mustSync(gameDir, Request{})
	addWorld(t, filepath.Join(gameDir, "saves"), "mine")
	h.add("sodium")
	h.mustSync(gameDir, Request{})
	if exists(filepath.Join(gameDir, instance.Dir, "backups")) {
		t.Fatal("a sync asked for no backup takes none")
	}
}

func TestAutomaticBackupsWithoutWorlds(t *testing.T) {
	h := newHarness(t)
	h.editManifest(func(m *manifest.Manifest) { m.Client.Build = "." })
	h.add("sodium")
	h.mustSync(h.dir, Request{Backup: "sync"})
	if exists(filepath.Join(h.dir, instance.Dir, "backups")) {
		t.Fatal("a worldless instance backs up as nothing")
	}
}

func TestAutomaticBackupsTrimToSaveBackups(t *testing.T) {
	h := newHarness(t)
	h.editManifest(func(m *manifest.Manifest) { m.Client.Build = "." })
	h.mustSync(h.dir, Request{Backup: "sync"})
	addWorld(t, filepath.Join(h.dir, "saves"), "mine")
	home := filepath.Join(h.dir, instance.Dir, "backups")
	for _, name := range []string{"20260910-000000-backup.zip", "20260911-000000-sync.zip", "20260912-000000-restore.zip", "20260913-000000-update.zip"} {
		writeFile(t, filepath.Join(home, name), "zip")
	}
	h.e.SaveBackups = 2
	h.add("sodium")
	h.mustSync(h.dir, Request{Backup: "sync"})
	if got := backupReasons(t, home); !slices.Equal(got, []string{"sync", "update", "restore", "backup"}) {
		t.Fatalf("the oldest automatic backup goes and the kept ones stay: %v", got)
	}

	h.e.SaveBackups = 0
	h.remove("sodium")
	h.mustSync(h.dir, Request{Backup: "sync"})
	if got := backupReasons(t, home); len(got) != 4 {
		t.Fatalf("0 takes no automatic backup: %v", got)
	}
}

func TestAutomaticBackupThatCantBeWrittenStopsTheSync(t *testing.T) {
	h := newHarness(t)
	h.editManifest(func(m *manifest.Manifest) { m.Client.Build = "." })
	h.mustSync(h.dir, Request{Backup: "sync"})
	addWorld(t, filepath.Join(h.dir, "saves"), "mine")
	if err := os.WriteFile(filepath.Join(h.dir, instance.Dir, "backups"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	h.add("sodium")
	if _, err := h.sync(h.dir, Request{Backup: "sync"}); err == nil {
		t.Fatal("a backup that can't be written must stop the sync")
	}
	if exists(h.modPath(h.dir)) {
		t.Fatal("no mod changes without the backup")
	}
}

func TestOneRunBacksEachDirectoryUpOnce(t *testing.T) {
	h := newHarness(t)
	gameDir := filepath.Join(t.TempDir(), "game")
	h.mustSync(gameDir, Request{Backup: "sync"})
	addWorld(t, filepath.Join(gameDir, "saves"), "mine")
	h.add("sodium")
	h.mustSync(gameDir, Request{Backup: "sync"})
	h.remove("sodium")
	h.mustSync(gameDir, Request{Backup: "sync"})
	if got := backupReasons(t, filepath.Join(gameDir, instance.Dir, "backups")); len(got) != 1 {
		t.Fatalf("a run zips a directory's worlds once however many syncs change its mods: %v", got)
	}
}
