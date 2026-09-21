package saves

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func frozen(t *testing.T, at time.Time) {
	t.Helper()
	prev := now
	now = func() time.Time { return at }
	t.Cleanup(func() { now = prev })
}

func zipEntries(t *testing.T, path string) ([]string, string) {
	t.Helper()
	r, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	names := []string{}
	for _, f := range r.File {
		names = append(names, f.Name)
	}
	slices.Sort(names)
	return names, r.Comment
}

func TestTakeZipsOnlyWorldsAtTheRoot(t *testing.T) {
	src, home := t.TempDir(), filepath.Join(t.TempDir(), "backups")
	world(t, src, "survival")
	world(t, src, "creative")
	if err := os.MkdirAll(filepath.Join(src, "survival", "region"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "survival", "region", "r.0.0.mca"), []byte("region"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "not-a-world"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 9, 18, 20, 30, 15, 0, time.Local)
	frozen(t, at)

	var zipped []string
	got, err := Take(Source{Dir: src, Instance: "pack", Minecraft: "26.2", Loader: "fabric"}, Home{Dir: home}, "backup", func(w string) { zipped = append(zipped, w) })
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != filepath.Join(home, "20260918-203015-backup.zip") || got.Reason != "backup" || got.Worlds != 2 {
		t.Fatalf("took %+v", got)
	}
	if strings.Join(zipped, ",") != "creative,survival" || strings.Join(got.Names, ",") != "creative,survival" {
		t.Fatalf("zipped %v, names %v", zipped, got.Names)
	}
	names, comment := zipEntries(t, got.Path)
	want := []string{"creative/", "creative/level.dat", "survival/", "survival/level.dat", "survival/region/", "survival/region/r.0.0.mca"}
	if !slices.Equal(names, want) {
		t.Fatalf("entries = %v", names)
	}
	var meta struct {
		Format    int      `json:"format"`
		Taken     string   `json:"taken"`
		Reason    string   `json:"reason"`
		Instance  string   `json:"instance"`
		Worlds    []string `json:"worlds"`
		Minecraft string   `json:"minecraft"`
		Loader    string   `json:"loader"`
	}
	if err := json.Unmarshal([]byte(comment), &meta); err != nil {
		t.Fatalf("comment %q: %v", comment, err)
	}
	if meta.Format != 1 || meta.Reason != "backup" || meta.Instance != "pack" || meta.Minecraft != "26.2" || meta.Loader != "fabric" || strings.Join(meta.Worlds, ",") != "creative,survival" {
		t.Fatalf("comment = %+v", meta)
	}
	if taken, err := time.Parse(time.RFC3339, meta.Taken); err != nil || !taken.Equal(at) {
		t.Fatalf("taken = %q", meta.Taken)
	}
}

func TestTakeNamesAndCountsCollisions(t *testing.T) {
	src, home := t.TempDir(), t.TempDir()
	world(t, src, "survival")
	frozen(t, time.Date(2026, 9, 18, 20, 30, 15, 0, time.Local))

	var paths []string
	for range 3 {
		got, err := Take(Source{Dir: src, Instance: "pack"}, Home{Dir: home, Shared: true}, "backup", nil)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, filepath.Base(got.Path))
	}
	want := []string{"20260918-203015-pack-backup.zip", "20260918-203015-2-pack-backup.zip", "20260918-203015-3-pack-backup.zip"}
	if !slices.Equal(paths, want) {
		t.Fatalf("names = %v", paths)
	}
	backups, err := Backups(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 3 || backups[0].ID != "20260918-203015-3-pack-backup" || backups[2].ID != "20260918-203015-pack-backup" {
		t.Fatalf("backups = %+v", backups)
	}
}

func TestTakeOnlyTheLevel(t *testing.T) {
	src, home := t.TempDir(), t.TempDir()
	world(t, src, "world")
	world(t, src, "old-world")
	got, err := Take(Source{Dir: src, Only: "world"}, Home{Dir: home}, "backup", nil)
	if err != nil {
		t.Fatal(err)
	}
	if names, _ := zipEntries(t, got.Path); !slices.Equal(names, []string{"world/", "world/level.dat"}) {
		t.Fatalf("entries = %v", names)
	}
}

func TestTakeFindsNothing(t *testing.T) {
	home := filepath.Join(t.TempDir(), "backups")
	for _, src := range []string{filepath.Join(t.TempDir(), "missing"), t.TempDir()} {
		got, err := Take(Source{Dir: src}, Home{Dir: home}, "backup", nil)
		if err != nil || got.Path != "" {
			t.Fatalf("took %+v, %v", got, err)
		}
	}
	src := t.TempDir()
	world(t, src, "old-world")
	if got, err := Take(Source{Dir: src, Only: "world"}, Home{Dir: home}, "backup", nil); err != nil || got.Path != "" {
		t.Fatalf("a missing level: %+v, %v", got, err)
	}
	if _, err := os.Stat(home); !os.IsNotExist(err) {
		t.Fatalf("nothing found makes no backups folder: %v", err)
	}
}

func TestBackupsReadTheZip(t *testing.T) {
	src, home := t.TempDir(), t.TempDir()
	world(t, src, "survival")
	world(t, src, "creative")
	frozen(t, time.Date(2026, 9, 18, 20, 30, 15, 0, time.Local))
	if _, err := Take(Source{Dir: src, Minecraft: "26.2", Loader: "fabric"}, Home{Dir: home}, "update", nil); err != nil {
		t.Fatal(err)
	}

	f, err := os.Create(filepath.Join(home, "20260917-101500-backup.zip"))
	if err != nil {
		t.Fatal(err)
	}
	w := zip.NewWriter(f)
	for _, name := range []string{"one/level.dat", "two/", "three/region/r.0.0.mca", "loose.txt"} {
		if _, err := w.Create(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := os.WriteFile(filepath.Join(home, "20260916-101500-sync.zip"), []byte("not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}

	backups, err := Backups(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 3 {
		t.Fatalf("backups = %+v", backups)
	}
	if b := backups[0]; b.Worlds != 2 || b.Minecraft != "26.2" || b.Loader != "fabric" {
		t.Fatalf("with a comment: %+v", b)
	}
	if b := backups[1]; b.Worlds != 3 || b.Minecraft != "" || b.Reason != "backup" {
		t.Fatalf("without a comment: %+v", b)
	}
	if b := backups[2]; b.Worlds != 0 || b.Reason != "sync" {
		t.Fatalf("an unreadable zip still lists: %+v", b)
	}
}
