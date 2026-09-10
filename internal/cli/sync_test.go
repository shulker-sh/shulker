package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shulker-sh/shulker/internal/build"
)

func TestSyncIntoDirectory(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	into := filepath.Join(t.TempDir(), "instance", "minecraft")
	stdout := h.mustRun(t, "sync", h.dir, "--into", into)
	if !strings.Contains(stdout, "client: ") || !strings.Contains(stdout, "into "+into) {
		t.Fatalf("sync output: %s", stdout)
	}
	for _, rel := range []string{"mods/" + h.jars["sodium"].filename, "mods/" + h.jars["fabric-api"].filename, "options.txt", build.StateFile} {
		if _, err := os.Stat(filepath.Join(into, rel)); err != nil {
			t.Fatalf("expected %s in the sync directory: %v", rel, err)
		}
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build")); !os.IsNotExist(err) {
		t.Fatalf("sync must not touch the project build directory: %v", err)
	}
	link, err := os.Readlink(filepath.Join(into, "saves"))
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.Rel(into, filepath.Join(h.dir, build.DataDir, "client", "saves"))
	if link != want {
		t.Fatalf("saves link = %q, want %q", link, want)
	}

	stdout = h.mustRun(t, "sync", h.dir, "--into", into)
	if !strings.Contains(stdout, "fetched 0 file(s)") || !strings.Contains(stdout, "0 written") {
		t.Fatalf("second sync output: %s", stdout)
	}

	var env struct {
		Data syncResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "sync", h.dir, "--into", into, "--target", "client", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if res := env.Data; res.Target != "client" || res.Dir != into || res.Build == nil || res.Build.Dir != into {
		t.Fatalf("json result: %+v", res)
	}
}

func TestSyncErrors(t *testing.T) {
	h := newHarness(t)
	code, stdout, _ := h.run(t, "sync", t.TempDir(), "--json")
	if code == 0 || failureCode(t, stdout).Code != "project-not-found" {
		t.Fatalf("missing project: exit %d %s", code, stdout)
	}

	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	code, stdout, _ = h.run(t, "sync", h.dir, "--target", "nope", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "target-not-found" || strings.Join(e.Candidates, ",") != "client" {
		t.Fatalf("unknown target: exit %d %s", code, stdout)
	}
}

func TestSyncFromGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	if err := os.MkdirAll(filepath.Join(h.dir, "overrides", "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.dir, "overrides", "config", "x.txt"), []byte("v1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, h.dir, "init", "-q", "-b", "main")
	gitRun(t, h.dir, "add", ".")
	gitRun(t, h.dir, "commit", "-q", "-m", "one")
	first := gitRun(t, h.dir, "rev-parse", "HEAD")
	source := "file://" + h.dir

	code, stdout, _ := h.run(t, "sync", source, "--json")
	if code == 0 || failureCode(t, stdout).Code != "into-required" {
		t.Fatalf("remote sync without --into: exit %d %s", code, stdout)
	}

	into := filepath.Join(t.TempDir(), "minecraft")
	var env struct {
		Data syncResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "sync", source, "--into", into, "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if res := env.Data; res.Kind != "git" || res.Commit != first || res.Source != source || res.Dir != into {
		t.Fatalf("json result: %+v", res)
	}
	for _, rel := range []string{"mods/" + h.jars["sodium"].filename, "config/x.txt", build.StateFile} {
		if _, err := os.Stat(filepath.Join(into, rel)); err != nil {
			t.Fatalf("expected %s in the sync directory: %v", rel, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(into, "saves")); !os.IsNotExist(err) {
		t.Fatalf("remote sync must not create data links: %v", err)
	}
	export := filepath.Join(h.cache, "packs", "src", first)
	if _, err := os.Stat(filepath.Join(export, build.DataDir)); !os.IsNotExist(err) {
		t.Fatalf("remote sync must not create a data directory in the cache export: %v", err)
	}

	if err := os.WriteFile(filepath.Join(h.dir, "overrides", "config", "x.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, h.dir, "commit", "-q", "-am", "two")
	second := gitRun(t, h.dir, "rev-parse", "HEAD")
	gitRun(t, h.dir, "tag", "-a", "v1", "-m", "v1", first)

	h.mustRun(t, "sync", source, "--into", into)
	if got, _ := os.ReadFile(filepath.Join(into, "config", "x.txt")); string(got) != "v2\n" {
		t.Fatalf("sync must follow the remote HEAD: %q", got)
	}
	stdout = h.mustRun(t, "sync", source, "--into", into, "--ref", "v1", "--json")
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if env.Data.Commit != first || env.Data.Commit == second {
		t.Fatalf("--ref v1 should sync %s, got %+v", first, env.Data)
	}
	if got, _ := os.ReadFile(filepath.Join(into, "config", "x.txt")); string(got) != "v1\n" {
		t.Fatalf("--ref v1 content: %q", got)
	}

	code, stdout, _ = h.run(t, "sync", source, "--into", into, "--ref", "nope", "--json")
	if code == 0 || failureCode(t, stdout).Code != "source-ref" {
		t.Fatalf("unknown ref: exit %d %s", code, stdout)
	}
	code, stdout, _ = h.run(t, "sync", h.dir, "--ref", "main", "--json")
	if code == 0 || failureCode(t, stdout).Code != "source-ref" {
		t.Fatalf("--ref on a local path: exit %d %s", code, stdout)
	}
}

func TestSyncFromManifestURL(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	files := map[string]bool{"shulker.json": true, "shulker.lock": true}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/pack/")
		if !files[name] {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(h.dir, name))
	}))
	defer srv.Close()
	source := srv.URL + "/pack/shulker.json"

	into := filepath.Join(t.TempDir(), "minecraft")
	var env struct {
		Data syncResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "sync", source, "--into", into, "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if res := env.Data; res.Kind != "url" || res.Source != source || res.Commit != "" {
		t.Fatalf("json result: %+v", res)
	}
	if _, err := os.Stat(filepath.Join(into, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(into, "saves")); !os.IsNotExist(err) {
		t.Fatalf("remote sync must not create data links: %v", err)
	}

	files["shulker.lock"] = false
	code, stdout, _ := h.run(t, "sync", source, "--into", into, "--json")
	if code == 0 || failureCode(t, stdout).Code != "source-lock" {
		t.Fatalf("missing lock: exit %d %s", code, stdout)
	}
	code, stdout, _ = h.run(t, "sync", srv.URL+"/pack/other.json", "--into", into, "--json")
	if code == 0 || failureCode(t, stdout).Code != "source-fetch" {
		t.Fatalf("missing manifest: exit %d %s", code, stdout)
	}
}
