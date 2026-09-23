package build

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/lock"
)

func TestHistoryLeavesPlacedDatapacksToTheCache(t *testing.T) {
	dir := t.TempDir()
	placed := []string{
		"datapacks/dp.zip",
		"config/paxi/datapacks/dp.zip",
		"config/openloader/data/dp.zip",
		"config/openloader/packs/dp.zip",
		"survival/datapacks/dp.zip",
		"other/datapacks/dp.zip",
		"config/openloader/data/mine.zip",
	}
	files := map[string]string{}
	for _, rel := range placed {
		files[rel] = "x"
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := fsutil.Write(filepath.Join(dir, rel), []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writeState(dir, State{Files: files}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, PropertiesFile), []byte("level-name=survival\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lk := &lock.Lock{Datapacks: map[string]lock.Pack{"dp": {Filename: "dp.zip"}}}
	cached := leftToCache(dir, lk)
	into := t.TempDir()
	if err := copyTree(dir, historyConfig, into, cached); err != nil {
		t.Fatal(err)
	}
	kept := managedFiles(dir, cached)
	for _, rel := range placed {
		if _, err := os.Stat(filepath.Join(into, rel)); err == nil {
			kept = append(kept, rel)
		}
	}
	if want := []string{"other/datapacks/dp.zip", "config/openloader/data/mine.zip"}; !slices.Equal(kept, want) {
		t.Fatalf("entry holds %v, want %v", kept, want)
	}
}
