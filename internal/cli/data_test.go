package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildLinksDataDirs(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	buildDir := filepath.Join(h.dir, "build", "client")
	if err := os.MkdirAll(filepath.Join(buildDir, "saves", "First"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "saves", "First", "level.dat"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := h.mustRun(t, "install")
	if !strings.Contains(stdout, "4 linked, 1 moved") || !strings.Contains(stdout, "moved: saves » data/client/saves") {
		t.Fatalf("first build: %s", stdout)
	}
	for _, rel := range []string{"saves", "screenshots", "logs", "crash-reports"} {
		target, err := os.Readlink(filepath.Join(buildDir, rel))
		if err != nil || target != filepath.Join("..", "..", "data", "client", rel) {
			t.Fatalf("%s: %q %v", rel, target, err)
		}
	}
	if _, err := os.Stat(filepath.Join(h.dir, "data", "client", "saves", "First", "level.dat")); err != nil {
		t.Fatal("moved save is missing:", err)
	}
	if _, err := os.Stat(filepath.Join(buildDir, "saves", "First", "level.dat")); err != nil {
		t.Fatal("save is not reachable through the link:", err)
	}

	stdout = h.mustRun(t, "build")
	if strings.Contains(stdout, "linked") || strings.Contains(stdout, " moved") {
		t.Fatalf("rebuild should not relink: %s", stdout)
	}

	if err := os.Remove(filepath.Join(buildDir, "logs")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(buildDir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(buildDir, "logs", "latest.log"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.dir, "data", "client", "logs", "old.log"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := h.run(t, "build", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "build-conflict" || len(e.Items) != 1 || !strings.HasPrefix(e.Items[0], "logs (exists in both") {
		t.Fatalf("both sides: exit %d %s", code, stdout)
	}
	if code, _, _ := h.run(t, "build", "--force"); code == 0 {
		t.Fatal("force must not resolve a data collision")
	}
}

func TestServerBuildLinksWorldByLevelName(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack", "--side", "server")
	buildDir := filepath.Join(h.dir, "build", "server")

	stdout := h.mustRun(t, "install")
	if !strings.Contains(stdout, "3 linked") {
		t.Fatalf("first build: %s", stdout)
	}
	if target, err := os.Readlink(filepath.Join(buildDir, "world")); err != nil || target != filepath.Join("..", "..", "data", "server", "world") {
		t.Fatalf("world link: %q %v", target, err)
	}

	h.editManifest(t, func(m map[string]any) {
		m["server"].(map[string]any)["properties"].(map[string]any)["level-name"] = "creative"
	})
	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "1 removed, 1 linked") {
		t.Fatalf("level-name change: %s", stdout)
	}
	if _, err := os.Lstat(filepath.Join(buildDir, "world")); !os.IsNotExist(err) {
		t.Fatal("stale world link should be gone")
	}
	if _, err := os.Stat(filepath.Join(h.dir, "data", "server", "world")); err != nil {
		t.Fatal("old world data must stay:", err)
	}
	if target, err := os.Readlink(filepath.Join(buildDir, "creative")); err != nil || target != filepath.Join("..", "..", "data", "server", "creative") {
		t.Fatalf("creative link: %q %v", target, err)
	}
}
