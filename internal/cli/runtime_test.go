package cli

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/mojang"
)

const fakeJava = `#!/bin/sh
if [ "$1" = "-version" ]; then
  echo 'openjdk version "@VERSION@" 2025-10-21' >&2
  exit 0
fi
printf '%s\n' "$@" > args.txt.tmp && mv args.txt.tmp args.txt
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

const runtimeHome = "jre.bundle/Contents/Home"

type fakeRuntime struct {
	version string
	files   map[string]string
	links   map[string]string
	dirs    []string
	corrupt string
	// mu guards hits, which several runtime downloads land on at once.
	mu      sync.Mutex
	hits    int
	missing bool
}

func newFakeRuntime() *fakeRuntime {
	return &fakeRuntime{
		version: "25.0.1",
		files: map[string]string{
			runtimeHome + "/bin/java":    strings.NewReplacer("@VERSION@", "25.0.1", "@EXIT@", "0").Replace(fakeJava),
			runtimeHome + "/lib/modules": "modules",
		},
		links: map[string]string{runtimeHome + "/legal/LICENSE": "../lib/modules"},
		dirs:  []string{"jre.bundle", "jre.bundle/Contents", runtimeHome, runtimeHome + "/bin", runtimeHome + "/lib", runtimeHome + "/legal"},
	}
}

func sha1Hex(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}

func (f *fakeRuntime) manifest(base string) []byte {
	files := map[string]any{}
	for _, d := range f.dirs {
		files[d] = map[string]any{"type": "directory"}
	}
	for name, content := range f.files {
		sha := sha1Hex([]byte(content))
		files[name] = map[string]any{
			"type":       "file",
			"executable": strings.HasSuffix(name, "/bin/java"),
			"downloads":  map[string]any{"raw": map[string]any{"sha1": sha, "size": len(content), "url": base + "/jrt/objects/" + sha}},
		}
	}
	for name, target := range f.links {
		files[name] = map[string]any{"type": "link", "target": target}
	}
	data, _ := json.Marshal(map[string]any{"files": files})
	return data
}

func (f *fakeRuntime) register(mux *http.ServeMux, base func() string) {
	mux.HandleFunc("/jrt/all.json", func(w http.ResponseWriter, _ *http.Request) {
		platform, _ := mojang.RuntimePlatform()
		if f.missing {
			writeJSON(w, map[string]any{platform: map[string]any{}})
			return
		}
		writeJSON(w, map[string]any{platform: map[string]any{"java-runtime-epsilon": []map[string]any{{
			"manifest": map[string]any{"sha1": sha1Hex(f.manifest(base())), "url": base() + "/jrt/epsilon.json"},
			"version":  map[string]any{"name": f.version},
		}}}})
	})
	mux.HandleFunc("/jrt/epsilon.json", func(w http.ResponseWriter, _ *http.Request) {
		w.Write(f.manifest(base()))
	})
	mux.HandleFunc("/jrt/objects/", func(w http.ResponseWriter, req *http.Request) {
		f.mu.Lock()
		f.hits++
		f.mu.Unlock()
		want := strings.TrimPrefix(req.URL.Path, "/jrt/objects/")
		for name, content := range f.files {
			if sha1Hex([]byte(content)) == want {
				if name == f.corrupt {
					content += "tampered"
				}
				w.Write([]byte(content))
				return
			}
		}
		http.NotFound(w, req)
	})
}

func (h *harness) managedJavaDir() string {
	return filepath.Join(h.cache, "java", "java-runtime-epsilon")
}

func (h *harness) readRuntimeMarker(t *testing.T) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.managedJavaDir(), ".shulker-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	return m
}

func writeMojangLauncher(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "launcher_profiles.json"), []byte(`{"profiles": {}, "version": 3}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func resolvedJava(t *testing.T, dir string) string {
	t.Helper()
	f, err := instance.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if f.Resolved == nil {
		return ""
	}
	return f.Resolved.Java
}

func TestLinkMojangRecordsTheManagedRuntime(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	launcherDir := writeMojangLauncher(t)
	gameDir := mojangGameDir(launcherDir, "pack")
	managed := filepath.Join(h.managedJavaDir(), filepath.FromSlash(runtimeHome), "bin", "java")

	_, stderr := h.mustRunStderr(t, "link", "mojang", "--launcher-dir", launcherDir)
	if !strings.Contains(stderr, "downloaded Java runtime 25.0.1") {
		t.Fatalf("link should download the runtime on its own line:\n%s", stderr)
	}
	if got := resolvedJava(t, gameDir); got != managed {
		t.Fatalf("resolved.java = %q, want %q", got, managed)
	}

	if err := os.RemoveAll(h.managedJavaDir()); err != nil {
		t.Fatal(err)
	}
	_, stderr = h.mustRunStderr(t, "sync")
	if !strings.Contains(stderr, "downloaded Java runtime 25.0.1") {
		t.Fatalf("a sync should ensure the runtime again:\n%s", stderr)
	}
	if got := resolvedJava(t, gameDir); got != managed {
		t.Fatalf("resolved.java after sync = %q, want %q", got, managed)
	}

	_, stderr = h.mustRunStderr(t, "sync", "--offline")
	if !strings.Contains(stderr, "offline, keeping the installed Java runtime java-runtime-epsilon 25.0.1") {
		t.Fatalf("an offline sync should keep the installed runtime:\n%s", stderr)
	}
	if got := resolvedJava(t, gameDir); got != managed {
		t.Fatalf("resolved.java offline = %q, want %q", got, managed)
	}
}

func TestClientJavaSettingStandsInForTheRuntime(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	launcherDir := writeMojangLauncher(t)
	gameDir := mojangGameDir(launcherDir, "pack")
	h.mustRun(t, "link", "mojang", "--launcher-dir", launcherDir)
	f, err := instance.Load(gameDir)
	if err != nil {
		t.Fatal(err)
	}
	f.Settings.Java = "/opt/java/bin/java"
	if err := f.Save(gameDir); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(h.managedJavaDir()); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "sync")
	if got := resolvedJava(t, gameDir); got != "/opt/java/bin/java" {
		t.Fatalf("resolved.java = %q, want the java setting", got)
	}
	if _, err := os.Stat(h.managedJavaDir()); err == nil {
		t.Fatal("a java setting should stop the runtime download")
	}
}

func TestLinkMojangNamesTheJavaFlagWhenNoRuntimeExists(t *testing.T) {
	h := newHarness(t)
	h.runtime.missing = true
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	launcherDir := writeMojangLauncher(t)
	code, stdout, stderr := h.run(t, "link", "mojang", "--launcher-dir", launcherDir)
	if code == 0 || !strings.Contains(stderr, "runtime-unavailable") || !strings.Contains(stderr, "Fix: shulker link mojang --java <path>") {
		t.Fatalf("link should fail with the link fix: code=%d\nstdout: %s\nstderr: %s", code, stdout, stderr)
	}
	if strings.Contains(stderr, "shulker.json") {
		t.Fatalf("a client should not be told to edit shulker.json:\n%s", stderr)
	}
}

func TestOtherLaunchersBringTheirOwnJava(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", "--launcher-dir", prismDir, "--name", "Friends")
	gameDir := filepath.Join(prismDir, "instances", "shulker-friends", "minecraft")
	if got := resolvedJava(t, gameDir); got != "" {
		t.Fatalf("a Prism instance should record no runtime, got %q", got)
	}
	if _, err := os.Stat(h.managedJavaDir()); err == nil {
		t.Fatal("a Prism link should not download the runtime")
	}
}
