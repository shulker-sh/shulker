package saves

import (
	"os"
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestRestoreReplacesOnlyTheZipsWorldsWhole(t *testing.T) {
	dir := t.TempDir()
	world(t, dir, "survival")
	if err := os.WriteFile(filepath.Join(dir, "survival", "old.mca"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	world(t, dir, "hardcore")
	path := filepath.Join(t.TempDir(), "b.zip")
	zipOf(t, path, []string{"survival/", "survival/level.dat", "survival/region/r.0.0.mca", "creative/level.dat"}, "")

	a, err := OpenArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if len(a.Worlds) != 2 || a.Worlds[0] != "creative" || a.Worlds[1] != "survival" {
		t.Fatalf("worlds come from the entries: %v", a.Worlds)
	}
	got, err := a.Restore(dir, a.Worlds, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := []Restored{{Name: "creative"}, {Name: "survival", Replaced: true}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("restored %+v", got)
	}
	if exists(filepath.Join(dir, "survival", "old.mca")) {
		t.Fatal("a replaced world kept a file from before")
	}
	for _, p := range []string{"survival/region/r.0.0.mca", "creative/level.dat", "hardcore/level.dat"} {
		if !exists(filepath.Join(dir, p)) {
			t.Fatalf("%s is missing", p)
		}
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Fatalf("left behind %v", entries)
	}
}

func TestRestoreOnlyTheWorldsAskedFor(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "saves")
	path := filepath.Join(t.TempDir(), "b.zip")
	zipOf(t, path, []string{"world/level.dat", "other/level.dat"}, "")
	a, err := OpenArchive(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if _, err := a.Restore(dir, []string{"world"}, nil); err != nil {
		t.Fatal(err)
	}
	if !exists(filepath.Join(dir, "world", "level.dat")) || exists(filepath.Join(dir, "other")) {
		t.Fatal("restored more than it was asked to")
	}
}

func TestOpenArchiveRefusesWhatIsNotWorlds(t *testing.T) {
	notZip := filepath.Join(t.TempDir(), "not.zip")
	if err := os.WriteFile(notZip, []byte("nope"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, entries := range map[string][]string{
		"a file at the root":  {"world/level.dat", "notes.txt"},
		"a folder that isn't": {"world/level.dat", "mods/sodium.jar"},
		"nothing at all":      {},
		"a path out":          {"../world/level.dat"},
	} {
		path := filepath.Join(t.TempDir(), "b.zip")
		zipOf(t, path, entries, "")
		if _, err := OpenArchive(path); out.CodeOf(err) != "backup-invalid" {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := OpenArchive(notZip); out.CodeOf(err) != "backup-invalid" {
		t.Errorf("not a zip: %v", err)
	}
}
