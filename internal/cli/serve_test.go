//go:build !windows

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestServeRunsServerAndStops(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack", "--target", "server")
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
	if !strings.Contains(stderr, "started server in") || !strings.Contains(stderr, "Java 25") {
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
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack", "--target", "server")
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
	if code == 0 || !strings.Contains(stderr, "‣ server") {
		t.Fatalf("expected target-not-found, got %d: %s", code, stderr)
	}
}

func TestManagedJava(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack", "--target", "server")
	h.mustRun(t, "add", "fabric-api")

	code, stdout, _ := h.run(t, "--json", "install")
	if code != 0 {
		t.Fatal(stdout)
	}
	var res struct {
		Data struct {
			Fetched []string `json:"fetched"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Data.Fetched, ",") != "fabric-server-launcher,java-runtime-epsilon 25.0.1" {
		t.Fatalf("fetched: %v", res.Data.Fetched)
	}
	home := filepath.Join(h.managedJavaDir(), filepath.FromSlash(runtimeHome))
	info, err := os.Stat(filepath.Join(home, "bin", "java"))
	if err != nil || info.Mode()&0o111 == 0 {
		t.Fatalf("managed java missing or not executable: %v %v", err, info)
	}
	if target, err := os.Readlink(filepath.Join(home, "legal", "LICENSE")); err != nil || target != filepath.FromSlash("../lib/modules") {
		t.Fatalf("symlink: %q %v", target, err)
	}
	if marker := h.readRuntimeMarker(t); marker["version"] != "25.0.1" || marker["home"] != filepath.FromSlash(runtimeHome) {
		t.Fatalf("marker: %v", marker)
	}
	if h.runtime.hits != 2 {
		t.Fatalf("expected 2 object downloads, got %d", h.runtime.hits)
	}

	if stdout := h.mustRun(t, "install"); h.runtime.hits != 2 {
		t.Fatalf("second install re-downloaded the runtime: %s (hits %d)", stdout, h.runtime.hits)
	}

	h.runtime.corrupt = runtimeHome + "/lib/modules"
	h.runtime.files[runtimeHome+"/lib/modules"] = "modules v2"
	h.runtime.version = "25.0.2"
	code, _, stderr := h.run(t, "install")
	if code == 0 || !strings.Contains(stderr, "sha1 mismatch") {
		t.Fatalf("corrupt download should fail: %d %s", code, stderr)
	}
	if marker := h.readRuntimeMarker(t); marker["version"] != "25.0.1" {
		t.Fatalf("failed refresh replaced the runtime: %v", marker)
	}
	h.runtime.corrupt = ""
	if _, stderr := h.mustRunStderr(t, "install"); !strings.Contains(stderr, "downloaded Java runtime") {
		t.Fatalf("changed runtime should be refetched: %s", stderr)
	}
	if marker := h.readRuntimeMarker(t); marker["version"] != "25.0.2" {
		t.Fatalf("marker after refresh: %v", marker)
	}
	if data, _ := os.ReadFile(filepath.Join(home, "lib", "modules")); string(data) != "modules v2" {
		t.Fatalf("refreshed file: %q", data)
	}

	h.stdin = strings.NewReader("stop\n")
	code, stdout, stderr = h.run(t, "--json", "serve", "--accept-eula")
	if code != 0 {
		t.Fatalf("serve: %d %s %s", code, stdout, stderr)
	}
	var served struct {
		Data struct {
			Java struct {
				Path  string `json:"path"`
				Major int    `json:"major"`
			} `json:"java"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &served); err != nil {
		t.Fatal(err)
	}
	if served.Data.Java.Path != filepath.Join(home, "bin", "java") || served.Data.Java.Major != 25 {
		t.Fatalf("serve used %+v", served.Data.Java)
	}

	if err := os.RemoveAll(h.managedJavaDir()); err != nil {
		t.Fatal(err)
	}
	hits := h.runtime.hits
	h.stdin = strings.NewReader("stop\n")
	_, stderr = h.mustRunStderr(t, "serve")
	if h.runtime.hits != hits+2 || !strings.Contains(stderr, "downloaded Java runtime 25.0.2 (2 files") {
		t.Fatalf("serve should download a missing runtime: %s", stderr)
	}
}

func TestManagedJavaUnavailable(t *testing.T) {
	h := newHarness(t)
	h.runtime.missing = true
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack", "--target", "server")
	_, stderr := h.mustRunStderr(t, "install")
	if !strings.Contains(stderr, "runtime") || !strings.Contains(stderr, "shulker.json") {
		t.Fatalf("install should warn about the missing runtime: %s", stderr)
	}
	h.stdin = strings.NewReader("stop\n")
	_, _, stderr = h.run(t, "serve", "--accept-eula")
	if !strings.Contains(stderr, "using java on PATH") {
		t.Fatalf("serve should fall back to PATH java: %s", stderr)
	}
}

func TestServeInstallsWhatTheLockNeeds(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack", "--target", "server", "--loader", "neoforge")
	h.editManifest(t, func(m map[string]any) { m["java"] = h.fakeJDK(t, "25.0.1", "0") })
	h.stdin = strings.NewReader("stop\n")
	stdout, _ := h.mustRunStderr(t, "serve", "--accept-eula")
	if !strings.Contains(stdout, "server stopped") {
		t.Fatalf("serve must fetch the server files itself, not stop at `shulker install`: %s", stdout)
	}
}
