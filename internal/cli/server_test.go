package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/andrewmast/shulker/internal/lock"
)

func TestServerTargetBuild(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack", "--target", "server")
	h.mustRun(t, "add", "fabric-api")
	h.editManifest(t, func(m map[string]any) {
		m["variables"] = map[string]any{"motd": "Welcome"}
		m["server"] = map[string]any{
			"eula":       true,
			"properties": map[string]any{"motd": "${motd} to pack", "max-players": 8, "pvp": false},
		}
	})

	stdout := h.mustRun(t, "install")
	if !strings.Contains(stdout, "fetched 2 file(s)") {
		t.Fatalf("install output: %s", stdout)
	}
	var l lock.Lock
	h.readJSON(t, "shulker.lock", &l)
	if l.Loader.Server == nil || l.Loader.Server.Installer != "1.1.2" || l.Loader.Server.Sha512 != h.serverJar.sha512 {
		t.Fatalf("lock loader: %+v", l.Loader)
	}

	buildDir := filepath.Join(h.dir, "build", "server")
	if _, err := os.Stat(filepath.Join(buildDir, "fabric-server-launch.jar")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(buildDir, "mods", "fabric-api-0.130.0+26.2.jar")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(buildDir, "eula.txt")); got != "eula=true\n" {
		t.Fatalf("eula.txt: %q", got)
	}
	propsPath := filepath.Join(buildDir, "server.properties")
	if got := readFile(t, propsPath); got != "max-players=8\nmotd=Welcome to pack\npvp=false\n" {
		t.Fatalf("server.properties: %q", got)
	}

	h.mustRun(t, "install")
	if h.serverJarHits != 1 {
		t.Fatalf("server jar downloaded %d times", h.serverJarHits)
	}

	gameRewritten := "#Minecraft server properties\n#Thu Sep 10 00:00:00 UTC 2026\npvp=false\nmotd=Welcome to pack\nview-distance=10\nmax-players=8\n"
	if err := os.WriteFile(propsPath, []byte(gameRewritten), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "server: 0 written, 4 unchanged, 0 kept, 0 removed") {
		t.Fatalf("rebuild after game rewrite: %s", stdout)
	}
	if got := readFile(t, propsPath); got != gameRewritten {
		t.Fatalf("game-written file was touched: %q", got)
	}

	h.editManifest(t, func(m map[string]any) {
		m["server"].(map[string]any)["properties"].(map[string]any)["max-players"] = 12
	})
	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "server: 1 written") {
		t.Fatalf("rebuild after manifest change: %s", stdout)
	}
	if got := readFile(t, propsPath); got != strings.Replace(gameRewritten, "max-players=8", "max-players=12", 1) {
		t.Fatalf("merged server.properties: %q", got)
	}

	if err := os.WriteFile(propsPath, []byte(strings.Replace(readFile(t, propsPath), "pvp=false", "pvp=true", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "kept server.properties") {
		t.Fatalf("rebuild after user edit: %s", stdout)
	}

	h.editManifest(t, func(m map[string]any) {
		m["server"].(map[string]any)["properties"].(map[string]any)["pvp"] = true
	})
	h.editManifest(t, func(m map[string]any) {
		m["server"].(map[string]any)["properties"].(map[string]any)["motd"] = "Changed"
	})
	code, stdout, _ := h.run(t, "build", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "build-conflict" || len(e.Candidates) != 1 || !strings.HasPrefix(e.Candidates[0], "server.properties") {
		t.Fatalf("conflict: exit %d %s", code, stdout)
	}
}

func (h *harness) editManifest(t *testing.T, edit func(m map[string]any)) {
	t.Helper()
	path := filepath.Join(h.dir, "shulker.json")
	var m map[string]any
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	edit(m)
	if data, err = json.MarshalIndent(m, "", "  "); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestServerBuildAlwaysWritesProperties(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack", "--target", "server")
	h.mustRun(t, "install")

	path := filepath.Join(h.dir, "build", "server", "server.properties")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "difficulty=easy\n" {
		t.Fatalf("expected the seeded server.properties, got %q, %v", data, err)
	}

	gameWritten := "#Minecraft server properties\ndifficulty=easy\nmotd=A Minecraft Server\npvp=true\n"
	if err := os.WriteFile(path, []byte(gameWritten), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout := h.mustRun(t, "build")
	if !strings.Contains(stdout, "0 written, 2 unchanged, 0 kept") {
		t.Fatalf("rebuild after game write: %s", stdout)
	}
	if data, _ := os.ReadFile(path); string(data) != gameWritten {
		t.Fatalf("game-written file was changed: %q", data)
	}

	h.editManifest(t, func(m map[string]any) {
		m["server"] = map[string]any{"eula": true, "properties": map[string]any{"difficulty": "easy", "pvp": false}}
	})
	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "2 written, 1 unchanged") {
		t.Fatalf("rebuild with owned key: %s", stdout)
	}
	if data, _ := os.ReadFile(path); string(data) != "#Minecraft server properties\ndifficulty=easy\nmotd=A Minecraft Server\npvp=false\n" {
		t.Fatalf("merged file: %q", data)
	}
}
