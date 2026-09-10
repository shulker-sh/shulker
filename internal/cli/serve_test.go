//go:build !windows

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fakeJava = `#!/bin/sh
if [ "$1" = "-version" ]; then
  echo 'openjdk version "@VERSION@" 2025-10-21' >&2
  exit 0
fi
printf '%s\n' "$@" > args.txt
echo "[Server] Done"
while read -r line; do
  echo "[Server] got: $line"
  [ "$line" = "stop" ] && exit @EXIT@
done
exit 1
`

func (h *harness) fakeJDK(t *testing.T, version, exit string) string {
	t.Helper()
	jdk := filepath.Join(t.TempDir(), "jdk")
	if err := os.MkdirAll(filepath.Join(jdk, "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	script := strings.NewReplacer("@VERSION@", version, "@EXIT@", exit).Replace(fakeJava)
	if err := os.WriteFile(filepath.Join(jdk, "bin", "java"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return jdk
}

func TestServeRunsServerAndStops(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack", "--target", "server")
	var m map[string]any
	h.readJSON(t, "shulker.json", &m)
	srv, _ := m["server"].(map[string]any)
	props, _ := srv["properties"].(map[string]any)
	if srv == nil || srv["eula"] != false || srv["memory"] != "4G" || props["difficulty"] != "easy" {
		t.Fatalf("init did not seed the server block: %v", m["server"])
	}
	h.mustRun(t, "add", "fabric-api")
	h.mustRun(t, "install")

	code, _, stderr := h.run(t, "serve")
	if code == 0 || !strings.Contains(stderr, "eula-required") && !strings.Contains(stderr, "eula") {
		t.Fatalf("expected eula error, got %d: %s", code, stderr)
	}

	jdk := h.fakeJDK(t, "25.0.1", "0")
	h.editManifest(t, func(m map[string]any) {
		m["java"] = jdk
		m["server"] = map[string]any{"eula": false, "memory": "2G", "jvmArgs": []any{"-Dshulker.test=1"}}
	})
	h.tty = true
	h.stdin = strings.NewReader("n\n")
	code, _, stderr = h.run(t, "serve")
	if code == 0 || !strings.Contains(stderr, "Accept and record") || !strings.Contains(stderr, "--accept-eula") {
		t.Fatalf("declined prompt: %d %s", code, stderr)
	}
	h.readJSON(t, "shulker.json", &m)
	if m["server"].(map[string]any)["eula"] != false {
		t.Fatal("declining must not change the manifest")
	}

	h.stdin = strings.NewReader("y\nsay hi\nstop\n")
	code, stdout, stderr := h.run(t, "serve")
	h.readJSON(t, "shulker.json", &m)
	if m["server"].(map[string]any)["eula"] != true {
		t.Fatal("accepting must record eula: true")
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build", "server", "eula.txt")); err != nil {
		t.Fatal("eula.txt not written after accepting")
	}
	h.tty = false
	if code != 0 {
		t.Fatalf("serve: %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "[Server] got: say hi") || !strings.Contains(stdout, "server stopped") {
		t.Fatalf("stdout: %s", stdout)
	}
	if !strings.Contains(stderr, "starting server in") || !strings.Contains(stderr, "Java 25") {
		t.Fatalf("stderr: %s", stderr)
	}
	args, err := os.ReadFile(filepath.Join(h.dir, "build", "server", "args.txt"))
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(args)), "\n")
	if got[0] != "-Xms2G" || got[1] != "-Xmx2G" || !strings.Contains(string(args), "-XX:+UseG1GC\n") {
		t.Fatalf("args: %v", got)
	}
	tail := strings.Join(got[len(got)-4:], " ")
	if tail != "-Dshulker.test=1 -jar fabric-server-launch.jar --nogui" {
		t.Fatalf("args tail: %s", tail)
	}

	h.stdin = strings.NewReader("stop\n")
	code, stdout, _ = h.run(t, "--json", "serve")
	if code != 0 {
		t.Fatalf("json serve: %d %s", code, stdout)
	}
	var env struct {
		OK   bool `json:"ok"`
		Data struct {
			Target   string `json:"target"`
			ExitCode int    `json:"exitCode"`
			Java     struct {
				Major int `json:"major"`
			} `json:"java"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("json stdout not a single envelope: %v\n%s", err, stdout)
	}
	if !env.OK || env.Data.Target != "server" || env.Data.ExitCode != 0 || env.Data.Java.Major != 25 {
		t.Fatalf("envelope: %+v", env)
	}
}

func TestServeErrors(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack", "--target", "server")
	h.mustRun(t, "install")

	old := h.fakeJDK(t, "17.0.12", "0")
	h.editManifest(t, func(m map[string]any) { m["java"] = old })
	code, _, stderr := h.run(t, "serve", "--accept-eula")
	var m map[string]any
	h.readJSON(t, "shulker.json", &m)
	if m["server"].(map[string]any)["eula"] != true {
		t.Fatal("--accept-eula must record eula: true")
	}
	if code == 0 || !strings.Contains(stderr, "needs Java 25") {
		t.Fatalf("expected java-version error, got %d: %s", code, stderr)
	}

	crash := h.fakeJDK(t, "25.0.1", "3")
	h.editManifest(t, func(m map[string]any) { m["java"] = crash })
	h.stdin = strings.NewReader("stop\n")
	code, _, stderr = h.run(t, "serve")
	if code != 3 || !strings.Contains(stderr, "server exited with status 3") {
		t.Fatalf("expected exit 3, got %d: %s", code, stderr)
	}

	code, _, stderr = h.run(t, "serve", "--target", "nope")
	if code == 0 || !strings.Contains(stderr, "candidates: server") {
		t.Fatalf("expected target-not-found, got %d: %s", code, stderr)
	}
}
