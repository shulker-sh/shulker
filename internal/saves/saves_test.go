package saves

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func world(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, name), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name, "level.dat"), []byte("nbt"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func linkTarget(t *testing.T, instDir string) string {
	t.Helper()
	target, err := os.Readlink(filepath.Join(instDir, "saves"))
	if err != nil {
		t.Fatalf("saves is not a link: %v", err)
	}
	return target
}

func TestLinkNewInstanceJoinsGroup(t *testing.T) {
	inst, root := t.TempDir(), t.TempDir()
	world(t, filepath.Join(root, "default"), "survival")

	res, err := Link(inst, root, Default)
	if err != nil {
		t.Fatal(err)
	}
	if got := linkTarget(t, inst); got != filepath.Join(root, "default") {
		t.Fatalf("saves points at %s", got)
	}
	if !res.Changed || res.Group != "default" || !slices.Equal(res.Worlds, []string{"survival"}) {
		t.Fatalf("result = %+v", res)
	}
}

func TestLinkAgainChangesNothing(t *testing.T) {
	inst, root := t.TempDir(), t.TempDir()
	if _, err := Link(inst, root, Default); err != nil {
		t.Fatal(err)
	}
	res, err := Link(inst, root, Default)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed {
		t.Fatalf("second link reported a change: %+v", res)
	}
}

func TestLinkSwitchesGroupWithoutCopying(t *testing.T) {
	inst, root := t.TempDir(), t.TempDir()
	world(t, filepath.Join(root, "default"), "survival")
	world(t, filepath.Join(root, "hardcore"), "one-life")
	if _, err := Link(inst, root, Default); err != nil {
		t.Fatal(err)
	}

	res, err := Link(inst, root, "hardcore")
	if err != nil {
		t.Fatal(err)
	}
	if got := linkTarget(t, inst); got != filepath.Join(root, "hardcore") {
		t.Fatalf("saves points at %s", got)
	}
	if !res.Changed || !slices.Equal(res.Worlds, []string{"one-life"}) {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "default", "survival", "level.dat")); err != nil {
		t.Fatalf("the old group lost its world: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "hardcore", "survival")); err == nil {
		t.Fatal("the old group's world was copied into the new one")
	}
}

func TestLinkNoneLeavesTheGroupBehind(t *testing.T) {
	inst, root := t.TempDir(), t.TempDir()
	world(t, filepath.Join(root, "default"), "survival")
	if _, err := Link(inst, root, Default); err != nil {
		t.Fatal(err)
	}

	res, err := Link(inst, root, None)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(filepath.Join(inst, "saves"))
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("saves is not a plain directory: %v %v", info, err)
	}
	if !res.Changed || res.Group != None || len(res.Worlds) != 0 {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "default", "survival", "level.dat")); err != nil {
		t.Fatalf("the group lost its world: %v", err)
	}
}

func TestLinkNoneKeepsOwnSaves(t *testing.T) {
	inst, root := t.TempDir(), t.TempDir()
	world(t, filepath.Join(inst, "saves"), "mine")

	res, err := Link(inst, root, None)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || !slices.Equal(res.Worlds, []string{"mine"}) {
		t.Fatalf("result = %+v", res)
	}
}

func TestLinkReplacesEmptySaves(t *testing.T) {
	inst, root := t.TempDir(), t.TempDir()
	if err := os.MkdirAll(filepath.Join(inst, "saves"), 0o755); err != nil {
		t.Fatal(err)
	}
	world(t, filepath.Join(root, "default"), "survival")

	res, err := Link(inst, root, Default)
	if err != nil {
		t.Fatal(err)
	}
	linkTarget(t, inst)
	if !res.Changed || res.Moved {
		t.Fatalf("result = %+v", res)
	}
}

func TestLinkMovesOwnWorldsIntoAnEmptyGroup(t *testing.T) {
	inst, root := t.TempDir(), t.TempDir()
	world(t, filepath.Join(inst, "saves"), "mine")
	if err := os.MkdirAll(filepath.Join(root, "default"), 0o755); err != nil {
		t.Fatal(err)
	}

	res, err := Link(inst, root, Default)
	if err != nil {
		t.Fatal(err)
	}
	linkTarget(t, inst)
	if !res.Changed || !res.Moved || !slices.Equal(res.Worlds, []string{"mine"}) {
		t.Fatalf("result = %+v", res)
	}
	if _, err := os.Stat(filepath.Join(root, "default", "mine", "level.dat")); err != nil {
		t.Fatalf("the world did not move into the group: %v", err)
	}
}

func TestLinkLeavesSavesWhenBothHoldWorlds(t *testing.T) {
	inst, root := t.TempDir(), t.TempDir()
	world(t, filepath.Join(inst, "saves"), "mine")
	world(t, filepath.Join(root, "default"), "survival")

	res, err := Link(inst, root, Default)
	if err != nil {
		t.Fatal(err)
	}
	if res.Changed || res.Conflict == "" || !slices.Equal(res.Worlds, []string{"mine"}) {
		t.Fatalf("result = %+v", res)
	}
	info, err := os.Lstat(filepath.Join(inst, "saves"))
	if err != nil || info.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("saves was replaced: %v %v", info, err)
	}
}

func TestWorldsSkipsFoldersWithoutLevelDat(t *testing.T) {
	dir := t.TempDir()
	world(t, dir, "b")
	world(t, dir, "a")
	if err := os.MkdirAll(filepath.Join(dir, "not-a-world"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Worlds(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(got, []string{"a", "b"}) {
		t.Fatalf("worlds = %v", got)
	}
	if got, err := Worlds(filepath.Join(dir, "missing")); err != nil || len(got) != 0 {
		t.Fatalf("missing dir: %v %v", got, err)
	}
}

func TestGroups(t *testing.T) {
	root := t.TempDir()
	world(t, filepath.Join(root, "default"), "survival")
	world(t, filepath.Join(root, "default"), "creative")
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := Groups(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Name != "default" || got[0].Worlds != 2 || got[0].Size == 0 || got[1].Name != "empty" || got[1].Worlds != 0 {
		t.Fatalf("groups = %+v", got)
	}
	if got, err := Groups(filepath.Join(root, "missing")); err != nil || len(got) != 0 {
		t.Fatalf("missing root: %v %v", got, err)
	}
}

func backup(t *testing.T, dir, name string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte("zip"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestBackupsNewestFirst(t *testing.T) {
	dir := t.TempDir()
	backup(t, dir, "20260917-101500-smp-sync.zip")
	backup(t, dir, "20260918-203015-smp-update.zip")
	backup(t, dir, "20260918-203015-2-smp-backup.zip")
	backup(t, dir, "notes.txt")

	got, err := Backups(dir)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, b := range got {
		ids = append(ids, b.ID)
	}
	want := []string{"20260918-203015-2-smp-backup", "20260918-203015-smp-update", "20260917-101500-smp-sync"}
	if !slices.Equal(ids, want) {
		t.Fatalf("ids = %v", ids)
	}
	if got[1].Reason != "update" || !got[1].Taken.Equal(time.Date(2026, 9, 18, 20, 30, 15, 0, time.Local)) || got[1].Size != 3 {
		t.Fatalf("backup = %+v", got[1])
	}
	if got, err := Backups(filepath.Join(dir, "missing")); err != nil || len(got) != 0 {
		t.Fatalf("missing dir: %v %v", got, err)
	}
}

func TestPruneKeepsNewest(t *testing.T) {
	dir := t.TempDir()
	backup(t, dir, "20260916-000000-sync.zip")
	backup(t, dir, "20260917-000000-sync.zip")
	backup(t, dir, "20260918-000000-backup.zip")

	pruned, err := Prune(dir, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(pruned) != 2 || pruned[0].ID != "20260917-000000-sync" || pruned[1].ID != "20260916-000000-sync" {
		t.Fatalf("pruned = %+v", pruned)
	}
	left, err := Backups(dir)
	if err != nil || len(left) != 1 || left[0].ID != "20260918-000000-backup" {
		t.Fatalf("left = %+v %v", left, err)
	}
	if pruned, err := Prune(dir, 5); err != nil || len(pruned) != 0 {
		t.Fatalf("second prune = %+v %v", pruned, err)
	}
}

func TestTrimAutomaticLeavesKeptBackups(t *testing.T) {
	dir := t.TempDir()
	backup(t, dir, "20260915-000000-backup.zip")
	backup(t, dir, "20260916-000000-sync.zip")
	backup(t, dir, "20260917-000000-restore.zip")
	backup(t, dir, "20260918-000000-smp-update.zip")
	backup(t, dir, "20260919-000000-sync.zip")
	backup(t, dir, "notes.zip")

	if err := TrimAutomatic(dir, 2); err != nil {
		t.Fatal(err)
	}
	left, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range left {
		names = append(names, e.Name())
	}
	want := []string{"20260915-000000-backup.zip", "20260917-000000-restore.zip", "20260918-000000-smp-update.zip", "20260919-000000-sync.zip", "notes.zip"}
	if !slices.Equal(names, want) {
		t.Fatalf("left = %v", names)
	}
}

func TestValidGroup(t *testing.T) {
	for _, name := range []string{"default", "hardcore", "smp-2"} {
		if !ValidGroup(name) {
			t.Errorf("%q should be a valid group", name)
		}
	}
	for _, name := range []string{"", "none", "../x", "A", ".hidden"} {
		if ValidGroup(name) {
			t.Errorf("%q should not be a valid group", name)
		}
	}
}
