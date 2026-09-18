package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
)

func TestServerTargetBuild(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack", "--side", "server")
	h.mustRun(t, "add", "fabric-api")
	h.editManifest(t, func(m map[string]any) {
		m["variables"] = map[string]any{"motd": "Welcome"}
		m["server"] = map[string]any{
			"eula":       true,
			"properties": map[string]any{"motd": "${motd} to pack", "max-players": 8, "online-mode": false},
		}
	})

	stdout, stderr := h.mustRunStderr(t, "install")
	if !strings.Contains(stderr, "downloaded the fabric server launcher") || !strings.Contains(stderr, "downloaded Java runtime") {
		t.Fatalf("install output: %s\n%s", stdout, stderr)
	}
	var l lock.Lock
	h.readJSON(t, "shulker.lock", &l)
	launcherURL := h.server.URL + "/fabric/versions/loader/26.2/0.17.3/1.1.2/server/jar"
	if l.Loader.Server == nil || l.Loader.Server.Installer != "1.1.2" || l.Loader.Server.URL != launcherURL || l.Loader.Server.Sha512 != h.serverJar.sha512 {
		t.Fatalf("lock loader: %+v", l.Loader)
	}
	if l.Server == nil || *l.Server != (lock.Download{URL: h.server.URL + "/piston-data/server.jar", Sha512: h.vanilla.sha512}) {
		t.Fatalf("lock server: %+v", l.Server)
	}
	lockWithURL := readFile(t, filepath.Join(h.dir, "shulker.lock"))
	l.Loader.Server.URL = ""
	if err := l.Save(filepath.Join(h.dir, "shulker.lock")); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "install")
	if got := readFile(t, filepath.Join(h.dir, "shulker.lock")); got != lockWithURL {
		t.Fatalf("install should write the launcher url back:\n%s", got)
	}

	buildDir := filepath.Join(h.dir, "build", "server")
	if _, err := os.Stat(filepath.Join(buildDir, "fabric-server-launch.jar")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(buildDir, ".fabric", "server", "26.2-server.jar")); got != string(h.vanilla.data) {
		t.Fatal(".fabric/server/26.2-server.jar is not the vanilla jar")
	}
	if _, err := os.Stat(filepath.Join(buildDir, "mods", "fabric-api-0.130.0+26.2.jar")); err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, filepath.Join(buildDir, "eula.txt")); got != "eula=true\n" {
		t.Fatalf("eula.txt: %q", got)
	}
	propsPath := filepath.Join(buildDir, "server.properties")
	if got := readFile(t, propsPath); got != "max-players=8\nmotd=Welcome to pack\nonline-mode=false\n" {
		t.Fatalf("server.properties: %q", got)
	}

	h.mustRun(t, "install")
	if h.serverJarHits != 1 {
		t.Fatalf("server jar downloaded %d times", h.serverJarHits)
	}

	gameRewritten := "#Minecraft server properties\n#Thu Sep 10 00:00:00 UTC 2026\nonline-mode=false\nmotd=Welcome to pack\nview-distance=10\nmax-players=8\n"
	if err := os.WriteFile(propsPath, []byte(gameRewritten), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "built server (5 unchanged)") {
		t.Fatalf("rebuild after game rewrite: %s", stdout)
	}
	if got := readFile(t, propsPath); got != gameRewritten {
		t.Fatalf("game-written file was touched: %q", got)
	}

	h.editManifest(t, func(m map[string]any) {
		m["server"].(map[string]any)["properties"].(map[string]any)["max-players"] = 12
	})
	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "built server (1 written") {
		t.Fatalf("rebuild after manifest change: %s", stdout)
	}
	if got := readFile(t, propsPath); got != strings.Replace(gameRewritten, "max-players=8", "max-players=12", 1) {
		t.Fatalf("merged server.properties: %q", got)
	}

	if err := os.WriteFile(propsPath, []byte(strings.Replace(readFile(t, propsPath), "online-mode=false", "online-mode=true", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "kept: server.properties online-mode (edited in place)") {
		t.Fatalf("rebuild after user edit: %s", stdout)
	}
	if got := readFile(t, propsPath); !strings.Contains(got, "online-mode=true") {
		t.Fatalf("in-game edit must survive a rebuild: %q", got)
	}

	h.editManifest(t, func(m map[string]any) {
		m["server"].(map[string]any)["properties"].(map[string]any)["motd"] = "Changed"
	})
	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "built server (1 written") || !strings.Contains(stdout, "kept: server.properties online-mode (edited in place)") {
		t.Fatalf("manifest change to another key must keep the edit: %s", stdout)
	}
	if got := readFile(t, propsPath); !strings.Contains(got, "online-mode=true") || !strings.Contains(got, "motd=Changed") {
		t.Fatalf("merged after unrelated manifest change: %q", got)
	}

	h.editManifest(t, func(m map[string]any) {
		m["server"].(map[string]any)["properties"].(map[string]any)["online-mode"] = true
	})
	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "built server (5 unchanged)") || strings.Contains(stdout, "kept") && strings.Contains(stdout, "online-mode") {
		t.Fatalf("manifest catching up to the edit: %s", stdout)
	}

	h.editManifest(t, func(m map[string]any) {
		m["server"].(map[string]any)["properties"].(map[string]any)["online-mode"] = false
	})
	if err := os.WriteFile(propsPath, []byte(strings.Replace(readFile(t, propsPath), "motd=Changed", "motd=Mine", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	h.editManifest(t, func(m map[string]any) {
		m["server"].(map[string]any)["properties"].(map[string]any)["motd"] = "Theirs"
	})
	code, stdout, _ := h.run(t, "build", "--json")
	if code != 0 {
		t.Fatalf("both-changed key must not fail the build: %s", stdout)
	}
	var env struct {
		Warnings []string `json:"warnings"`
		Data     []struct {
			Written []string `json:"written"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Data) != 1 || len(env.Data[0].Written) != 1 || len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "motd was edited in place and changed in the manifest") {
		t.Fatalf("both-changed key: %+v", env.Data)
	}
	if got := readFile(t, propsPath); !strings.Contains(got, "motd=Theirs") || !strings.Contains(got, "online-mode=false") {
		t.Fatalf("manifest must win a both-changed key: %q", got)
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
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack", "--side", "server")
	h.mustRun(t, "install")

	path := filepath.Join(h.dir, "build", "server", "server.properties")
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "difficulty=easy\n" {
		t.Fatalf("expected the seeded server.properties, got %q, %v", data, err)
	}

	gameWritten := "#Minecraft server properties\ndifficulty=easy\nmotd=A Minecraft Server\nonline-mode=true\n"
	if err := os.WriteFile(path, []byte(gameWritten), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout := h.mustRun(t, "build")
	if !strings.Contains(stdout, "built server (3 unchanged)") {
		t.Fatalf("rebuild after game write: %s", stdout)
	}
	if data, _ := os.ReadFile(path); string(data) != gameWritten {
		t.Fatalf("game-written file was changed: %q", data)
	}

	h.editManifest(t, func(m map[string]any) {
		m["server"] = map[string]any{"eula": true, "properties": map[string]any{"difficulty": "easy", "online-mode": false}}
	})
	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "2 written, 2 unchanged") {
		t.Fatalf("rebuild with owned key: %s", stdout)
	}
	if data, _ := os.ReadFile(path); string(data) != "#Minecraft server properties\ndifficulty=easy\nmotd=A Minecraft Server\nonline-mode=false\n" {
		t.Fatalf("merged file: %q", data)
	}

	h.editManifest(t, func(m map[string]any) {
		m["server"] = map[string]any{"eula": true, "properties": map[string]any{"online-mode": false}}
	})
	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "1 written, 3 unchanged") {
		t.Fatalf("rebuild after dropping a key: %s", stdout)
	}
	if data, _ := os.ReadFile(path); string(data) != "#Minecraft server properties\nmotd=A Minecraft Server\nonline-mode=false\n" {
		t.Fatalf("file after dropping difficulty: %q", data)
	}
	if stdout = h.mustRun(t, "build"); !strings.Contains(stdout, "built server (4 unchanged)") {
		t.Fatalf("rebuild after drop should be clean: %s", stdout)
	}
}

func TestServerBuildValidatesPropertyKeys(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack", "--side", "server")
	h.editManifest(t, func(m map[string]any) {
		m["server"] = map[string]any{"properties": map[string]any{"difficulty": "easy", "pvp": false, "vew-distance": 8}}
	})
	code, stdout, _ := h.run(t, "install", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "properties-invalid" || len(e.Items) != 1 || e.Items[0] != "pvp (removed in 1.21.9; use the pvp game rule)" {
		t.Fatalf("removed key: exit %d %s", code, stdout)
	}

	h.editManifest(t, func(m map[string]any) {
		props := m["server"].(map[string]any)["properties"].(map[string]any)
		delete(props, "pvp")
		props["view-distance"] = 64
	})
	code, stdout, stderr := h.run(t, "install")
	if !strings.Contains(stderr, `! server.properties key "view-distance" is 64, outside 3-32; the game clamps it`) {
		t.Fatalf("out of range: %s", stderr)
	}
	if code != 0 || !strings.Contains(stderr, `! server.properties key "vew-distance" is not a known key; did you mean "view-distance"?`) {
		t.Fatalf("unknown key: exit %d %s %s", code, stdout, stderr)
	}
	if got := readFile(t, filepath.Join(h.dir, "build", "server", "server.properties")); got != "difficulty=easy\nvew-distance=8\nview-distance=64\n" {
		t.Fatalf("server.properties: %q", got)
	}
}
