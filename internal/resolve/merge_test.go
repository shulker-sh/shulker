package resolve

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/packarchive"
	"shulker.sh/shulker/internal/project"
)

func TestMergeFailsOnALocalFileThePackLacks(t *testing.T) {
	dir := t.TempDir()
	p := &project.Project{Dir: dir, Manifest: &manifest.Manifest{Name: "p", Requires: map[string]manifest.Require{}}, Lock: lock.New()}
	pl := lock.New()
	pl.Mods["gone"] = lock.Mod{File: "files/gone.jar"}
	inc := &Incoming{Manifest: &manifest.Manifest{Name: "pack", Requires: map[string]manifest.Require{"gone": {File: "files/gone.jar"}}}, Lock: pl, Dir: t.TempDir()}
	if _, err := Merge(p, inc, []string{"client"}); out.CodeOf(err) != "local-file-missing" {
		t.Fatalf("got %v", err)
	}
}

func TestMergeReplacesWhatTheEarlierImportWroteAndKeepsWhatTheUserChanged(t *testing.T) {
	dir := t.TempDir()
	sha := func(c string) string { return strings.Repeat(c, 128) }
	p := &project.Project{Dir: dir, Manifest: &manifest.Manifest{Name: "p", Requires: map[string]manifest.Require{"a": {}, "b": {}}}, Lock: lock.New()}
	p.Lock.Mods["a"] = lock.Mod{Provider: "modrinth", Project: "pa", Version: "a1", Sha512: sha("1")}
	p.Lock.Mods["b"] = lock.Mod{Provider: "modrinth", Project: "pb", Version: "b9", Sha512: sha("9")}
	write := func(rel, data string) {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("overrides/config/x.cfg", "x old")
	write("overrides/config/y.cfg", "y mine")
	earlier := NewEarlier(&packarchive.Archive{
		Files: []packarchive.File{{Hashes: map[string]string{"sha512": sha("1")}}, {Hashes: map[string]string{"sha512": sha("2")}}},
		Overrides: []packarchive.Override{
			{Layer: "overrides", Path: "config/x.cfg", Data: []byte("x old")},
			{Layer: "overrides", Path: "config/y.cfg", Data: []byte("y old")},
		},
	})
	pl := lock.New()
	pl.Mods["a"] = lock.Mod{Provider: "modrinth", Project: "pa", Version: "a3", Sha512: sha("3")}
	pl.Mods["b"] = lock.Mod{Provider: "modrinth", Project: "pb", Version: "b4", Sha512: sha("4")}
	inc := &Incoming{
		Manifest: &manifest.Manifest{Name: "pack", Requires: map[string]manifest.Require{"a": {}, "b": {}}},
		Lock:     pl,
		Overrides: []packarchive.Override{
			{Layer: "overrides", Path: "config/x.cfg", Data: []byte("x new")},
			{Layer: "overrides", Path: "config/y.cfg", Data: []byte("y new")},
		},
		Earlier: earlier,
	}

	rep, err := Merge(p, inc, []string{"client"})
	if err != nil {
		t.Fatal(err)
	}

	if got := p.Lock.Mods["a"].Version; got != "a3" {
		t.Errorf("a still holds the earlier import's file, so it takes the pack's: %s", got)
	}
	if got := p.Lock.Mods["b"].Version; got != "b9" {
		t.Errorf("b was changed by the user, so it is kept: %s", got)
	}
	if want := []string{"b", "overrides/config/y.cfg"}; !slices.Equal(rep.KeptYours, want) {
		t.Errorf("kept yours = %q, want %q", rep.KeptYours, want)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "overrides", "config", "x.cfg")); string(data) != "x new" {
		t.Errorf("x.cfg = %q, want the pack's", data)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "overrides", "config", "y.cfg")); string(data) != "y mine" {
		t.Errorf("y.cfg = %q, want the user's", data)
	}
	rep.Undo()
	if data, _ := os.ReadFile(filepath.Join(dir, "overrides", "config", "x.cfg")); string(data) != "x old" {
		t.Errorf("undo leaves x.cfg %q, want it back as it was", data)
	}
}

func TestMergeMovesADependencyToThePacksKeyForAReplacedEntry(t *testing.T) {
	sha := func(c string) string { return strings.Repeat(c, 128) }
	p := &project.Project{Dir: t.TempDir(), Manifest: &manifest.Manifest{Name: "p", Requires: map[string]manifest.Require{"old-key": {}}}, Lock: lock.New()}
	p.Lock.Mods["old-key"] = lock.Mod{ModID: "shiny", Sha512: sha("1")}
	p.Lock.Mods["lib"] = lock.Mod{Sha512: sha("5"), RequiredBy: []string{"old-key"}}
	pl := lock.New()
	pl.Mods["shiny"] = lock.Mod{Sha512: sha("2")}
	inc := &Incoming{
		Manifest: &manifest.Manifest{Name: "pack", Requires: map[string]manifest.Require{"shiny": {}}},
		Lock:     pl,
		Earlier:  NewEarlier(&packarchive.Archive{Files: []packarchive.File{{Hashes: map[string]string{"sha512": sha("1")}}}}),
	}

	if _, err := Merge(p, inc, []string{"client"}); err != nil {
		t.Fatal(err)
	}

	if _, ok := p.Lock.Mods["old-key"]; ok {
		t.Fatalf("old-key was the earlier import's, so the pack's shiny takes its place: %v", p.Lock.Mods)
	}
	if got := p.Lock.Mods["lib"].RequiredBy; !slices.Equal(got, []string{"shiny"}) {
		t.Fatalf("lib is now required by the pack's key: %q", got)
	}
}
