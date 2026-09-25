package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
)

func newInPlace(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.editManifest(t, func(m map[string]any) {
		m["client"].(map[string]any)["build"] = "."
	})
	return h
}

func TestBuildInPlace(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "install")

	for _, rel := range []string{"mods/" + h.jars["sodium"].filename, "options.txt", filepath.Join(build.StateDir, build.StateFile)} {
		if _, err := os.Stat(filepath.Join(h.dir, rel)); err != nil {
			t.Fatalf("expected %s in the project directory: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build")); !os.IsNotExist(err) {
		t.Fatalf("an in-place build must not use build/: %v", err)
	}
	// No data links: saves and friends stay ordinary folders the build never touches.
	if _, err := os.Lstat(filepath.Join(h.dir, build.DataDir)); !os.IsNotExist(err) {
		t.Fatalf("an in-place build must not move data dirs: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(h.dir, "saves")); !os.IsNotExist(err) {
		t.Fatalf("an in-place build must not link saves: %v", err)
	}

	saves := filepath.Join(h.dir, "saves", "world")
	if err := os.MkdirAll(saves, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(saves, "level.dat"), []byte("level"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "remove", "sodium")
	h.mustRun(t, "build")
	if _, err := os.Stat(filepath.Join(h.dir, "mods", h.jars["sodium"].filename)); !os.IsNotExist(err) {
		t.Fatalf("a removed mod should be swept from the project directory: %v", err)
	}
	for _, rel := range []string{"shulker.json", "shulker.lock", filepath.Join("saves", "world", "level.dat")} {
		if _, err := os.Stat(filepath.Join(h.dir, rel)); err != nil {
			t.Fatalf("%s must survive an in-place build: %v", rel, err)
		}
	}
}

func TestBuildInPlaceRefusesReservedPaths(t *testing.T) {
	h := newInPlace(t)
	for _, rel := range []string{"shulker.json", filepath.Join("saves", "world.txt")} {
		path := filepath.Join(h.dir, "overrides", rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("mine"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, stdout, _ := h.run(t, "--json", "install")
	e := failureCode(t, stdout)
	if code == 0 || e.Code != "build-reserved" || len(e.Items) != 2 {
		t.Fatalf("reserved paths: code=%d %+v", code, e)
	}
	if strings.Join(e.Items, ",") != "saves/world.txt,shulker.json" {
		t.Fatalf("items: %+v", e.Items)
	}
	_, out, errOut := h.run(t, "install")
	if !strings.Contains(out+errOut, "2 files it doesn't own") {
		t.Fatalf("two reserved paths: %s", out+errOut)
	}
	if err := os.Remove(filepath.Join(h.dir, "overrides", "saves", "world.txt")); err != nil {
		t.Fatal(err)
	}
	if _, out, errOut = h.run(t, "install"); !strings.Contains(out+errOut, "1 file it doesn't own") {
		t.Fatalf("one reserved path: %s", out+errOut)
	}
}
