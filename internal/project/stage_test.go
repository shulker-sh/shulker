package project

import (
	"os"
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func stageFile(t *testing.T, s *Staged, rel, data string) {
	t.Helper()
	path := filepath.Join(s.Dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readString(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestAStagedProjectMovesIntoAFolderThatIsNotThereYet(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pack")
	s, err := Stage(dir)
	if err != nil {
		t.Fatal(err)
	}
	stageFile(t, s, "shulker.json", "{}")
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("nothing lands before the commit: %v", err)
	}
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}
	if readString(t, filepath.Join(dir, "shulker.json")) != "{}" {
		t.Fatal("the staged file should be in the folder")
	}
	if _, err := os.Stat(s.Dir); !os.IsNotExist(err) {
		t.Fatalf("the staging folder is gone: %v", err)
	}
}

func TestAStagedProjectMergesIntoAFolderThatHasOtherFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "downloads"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "downloads", "a.jar"), []byte("a"), 0o644)
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("old"), 0o644)
	s, err := Stage(dir)
	if err != nil {
		t.Fatal(err)
	}
	stageFile(t, s, ".gitignore", "new")
	stageFile(t, s, "overrides/config/a.toml", "a")
	if err := s.Commit(); err != nil {
		t.Fatal(err)
	}
	if readString(t, filepath.Join(dir, ".gitignore")) != "new" || readString(t, filepath.Join(dir, "overrides", "config", "a.toml")) != "a" || readString(t, filepath.Join(dir, "downloads", "a.jar")) != "a" {
		t.Fatal("staged files replace theirs, and the folder's own stay")
	}
}

func TestAStagedFolderWhereAFileIsMovesNothing(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "overrides"), []byte("a file"), 0o644)
	s, err := Stage(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Discard()
	stageFile(t, s, "shulker.json", "{}")
	stageFile(t, s, "overrides/a.txt", "a")
	if err := s.Commit(); out.CodeOf(err) != "path-taken" {
		t.Fatalf("err = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "shulker.json")); !os.IsNotExist(err) {
		t.Fatalf("a clash moves nothing: %v", err)
	}
}

func TestADiscardedStageLeavesNothing(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "pack")
	s, err := Stage(dir)
	if err != nil {
		t.Fatal(err)
	}
	stageFile(t, s, "shulker.json", "{}")
	s.Discard()
	entries, _ := os.ReadDir(filepath.Dir(dir))
	if len(entries) != 0 {
		t.Fatalf("left behind: %v", entries)
	}
}
