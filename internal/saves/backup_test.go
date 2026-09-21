package saves

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
		Format     int      `json:"format"`
		Taken      string   `json:"taken"`
		Reason     string   `json:"reason"`
		Instance   string   `json:"instance"`
		WorldCount int      `json:"worldCount"`
		Worlds     []string `json:"worlds"`
		Minecraft  string   `json:"minecraft"`
		Loader     string   `json:"loader"`
	}
	if err := json.Unmarshal([]byte(comment), &meta); err != nil {
		t.Fatalf("comment %q: %v", comment, err)
	}
	if meta.Format != 1 || meta.Reason != "backup" || meta.Instance != "pack" || meta.WorldCount != 2 || meta.Minecraft != "26.2" || meta.Loader != "fabric" || strings.Join(meta.Worlds, ",") != "creative,survival" {
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

func zipOf(t *testing.T, path string, entries []string, comment string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	w := zip.NewWriter(f)
	for _, name := range entries {
		if _, err := w.Create(name); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.SetComment(comment); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

type countingReader struct {
	r    io.ReaderAt
	read int64
}

func (c *countingReader) ReadAt(p []byte, off int64) (int, error) {
	n, err := c.r.ReadAt(p, off)
	c.read += int64(n)
	return n, err
}

func TestBackupsReadOnlyTheTail(t *testing.T) {
	path := filepath.Join(t.TempDir(), "20260918-203015-update.zip")
	var entries []string
	for i := range 5000 {
		entries = append(entries, fmt.Sprintf("world/region/r.%d.0.mca", i))
	}
	zipOf(t, path, entries, `{"format":1,"taken":"2026-09-18T18:30:15Z","reason":"update","worldCount":1,"worlds":["world"]}`)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() < 4*maxTail {
		t.Fatalf("zip of %d bytes is too small to tell", info.Size())
	}
	c := &countingReader{r: f}
	b, ok := readBackup(c, info.Size(), "20260918-203015-update")
	if !ok || b.Worlds != 1 || b.Reason != "update" {
		t.Fatalf("backup = %+v, %v", b, ok)
	}
	if c.read > maxTail {
		t.Fatalf("read %d bytes of %d", c.read, info.Size())
	}
}

func TestBackupsTrustTheComment(t *testing.T) {
	home := t.TempDir()
	zipOf(t, filepath.Join(home, "20260920-100000-backup.zip"), []string{"world/level.dat"}, `{"format":1,"taken":"2026-09-15T10:00:00Z","reason":"update","instance":"pack","worldCount":7,"worlds":["a","b","c","d","e","f","g"],"minecraft":"26.2","loader":"fabric"}`)
	zipOf(t, filepath.Join(home, "20260916-100000-sync.zip"), []string{"world/level.dat"}, "")
	zipOf(t, filepath.Join(home, "copied-in.zip"), []string{"world/level.dat"}, `{"format":1,"taken":"2026-09-14T10:00:00Z","reason":"restore","worldCount":1}`)

	backups, err := Backups(home)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, b := range backups {
		ids = append(ids, b.ID)
	}
	if !slices.Equal(ids, []string{"20260916-100000-sync", "20260920-100000-backup", "copied-in"}) {
		t.Fatalf("ids = %v", ids)
	}
	b := backups[1]
	if b.Reason != "update" || !b.Taken.Equal(time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)) || b.Taken.Location() != time.Local || b.Instance != "pack" || b.Worlds != 7 || len(b.Names) != 7 || b.Minecraft != "26.2" || b.Loader != "fabric" {
		t.Fatalf("from the comment: %+v", b)
	}
	if c := backups[2]; c.Reason != "restore" || c.Worlds != 1 || c.Names != nil {
		t.Fatalf("copied in: %+v", c)
	}

	pruned, err := Prune(home, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(pruned) != 2 || pruned[0].ID != "20260920-100000-backup" {
		t.Fatalf("pruned = %+v", pruned)
	}
}

func TestBackupsFallBackOnTheWholeComment(t *testing.T) {
	home := t.TempDir()
	roots := []string{"one/level.dat", "two/", "loose.txt"}
	zipOf(t, filepath.Join(home, "20260918-100000-sync.zip"), roots, "not json")
	zipOf(t, filepath.Join(home, "20260917-100000-pack-sync.zip"), roots, `{"taken":"2026-09-01T00:00:00Z","reason":"update","worldCount":9,"minecraft":"26.2"}`)
	zipOf(t, filepath.Join(home, "20260916-100000-2-backup.zip"), roots, "")
	zipOf(t, filepath.Join(home, "someone-elses.zip"), roots, "")

	backups, err := Backups(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(backups) != 3 {
		t.Fatalf("backups = %+v", backups)
	}
	want := []struct{ reason, instance string }{{"sync", ""}, {"sync", "pack"}, {"backup", ""}}
	for i, b := range backups {
		if b.Reason != want[i].reason || b.Instance != want[i].instance || b.Worlds != 2 || b.Minecraft != "" || b.Names != nil {
			t.Fatalf("backup %d = %+v", i, b)
		}
	}
	if !backups[1].Taken.Equal(time.Date(2026, 9, 17, 10, 0, 0, 0, time.Local)) || backups[2].seq != 2 {
		t.Fatalf("from the filename: %+v", backups[1:])
	}
}

func TestTailCommentWithTheSignatureInside(t *testing.T) {
	path := filepath.Join(t.TempDir(), "b.zip")
	fake := "PK\x05\x06" + strings.Repeat("\x00", 16) + "\x05\x00"
	for _, want := range []string{"a " + fake + "hello", "a " + fake + "hello, and more", "PK\x05\x06"} {
		zipOf(t, path, []string{"world/level.dat"}, want)
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		got, ok := tailComment(bytes.NewReader(data), int64(len(data)))
		if !ok || got != want {
			t.Fatalf("comment = %q, %v; want %q", got, ok, want)
		}
	}
	if _, ok := tailComment(bytes.NewReader([]byte("zip")), 3); ok {
		t.Fatal("found a comment in a file that isn't a zip")
	}
}

func TestTakeDropsNamesThatOverflowTheComment(t *testing.T) {
	src, home := t.TempDir(), t.TempDir()
	for i := range 300 {
		world(t, src, fmt.Sprintf("%03d-%s", i, strings.Repeat("w", 220)))
	}
	got, err := Take(Source{Dir: src}, Home{Dir: home}, "backup", nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Worlds != 300 || len(got.Names) != 300 {
		t.Fatalf("took %+v", got)
	}
	_, raw := zipEntries(t, got.Path)
	var meta map[string]any
	if err := json.Unmarshal([]byte(raw), &meta); err != nil {
		t.Fatalf("comment %q: %v", raw, err)
	}
	if _, ok := meta["worlds"]; ok || meta["worldCount"] != 300.0 {
		t.Fatalf("comment = %v", meta)
	}
	backups, err := Backups(home)
	if err != nil || len(backups) != 1 || backups[0].Worlds != 300 || backups[0].Names != nil || backups[0].Reason != "backup" {
		t.Fatalf("backups = %+v, %v", backups, err)
	}
	if c := commentFor(comment{Format: commentFormat, Instance: strings.Repeat("x", 70000)}); c != "" {
		t.Fatalf("an oversize comment = %d bytes", len(c))
	}
}
