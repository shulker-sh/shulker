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

	"shulker.sh/shulker/internal/build"
)

func TestSyncIntoDirectory(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	into := filepath.Join(t.TempDir(), "instance", "minecraft")
	stdout := h.mustRun(t, "sync", h.dir, "--into", into)
	if !strings.Contains(stdout, "synced client » "+into) {
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
	if _, err := os.Lstat(filepath.Join(into, "saves")); !os.IsNotExist(err) {
		t.Fatalf("a synced directory keeps its own saves, unlinked: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.dir, build.DataDir)); !os.IsNotExist(err) {
		t.Fatalf("sync must not write into the source project: %v", err)
	}

	stdout = h.mustRun(t, "sync", h.dir, "--into", into)
	if !strings.Contains(stdout, "(4 unchanged)") {
		t.Fatalf("second sync output: %s", stdout)
	}

	options := filepath.Join(into, "options.txt")
	data, err := os.ReadFile(options)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(options, []byte(strings.Replace(string(data), "tutorialStep:none", "tutorialStep:movement", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout = h.mustRun(t, "sync", h.dir, "--into", into)
	if !strings.Contains(stdout, "1 kept") || !strings.Contains(stdout, "kept: options.txt tutorialStep") {
		t.Fatalf("sync should list the kept key: %s", stdout)
	}

	var env struct {
		Warnings []string   `json:"warnings"`
		Data     syncResult `json:"data"`
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
	if code == 0 || failureCode(t, stdout).Code != "manifest-not-found" {
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
		Warnings []string   `json:"warnings"`
		Data     syncResult `json:"data"`
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
	if st := build.LoadState(into); st.Origin != (build.Origin{Source: source, Ref: "v1", Commit: first}) {
		t.Fatalf("state origin: %+v", st.Origin)
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

func TestSyncFromUnreachableGitUsesTheCache(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	gitRun(t, h.dir, "init", "-q", "-b", "main")
	gitRun(t, h.dir, "add", ".")
	gitRun(t, h.dir, "commit", "-q", "-m", "one")
	good := gitRun(t, h.dir, "rev-parse", "HEAD")
	served := t.TempDir()
	remote := filepath.Join(served, "remote.git")
	gitRun(t, h.dir, "clone", "-q", "--bare", h.dir, remote)
	gitRun(t, remote, "update-server-info")
	failing := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if failing {
			http.Error(w, "broken", http.StatusInternalServerError)
			return
		}
		http.FileServer(http.Dir(served)).ServeHTTP(w, r)
	}))
	source := srv.URL + "/remote.git"
	into := filepath.Join(t.TempDir(), "minecraft")
	h.mustRun(t, "sync", source, "--into", into)

	if err := os.WriteFile(filepath.Join(h.dir, "shulker.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, h.dir, "commit", "-q", "-am", "broken")
	gitRun(t, h.dir, "push", "-q", remote, "main")
	gitRun(t, remote, "update-server-info")
	if code, stdout, _ := h.run(t, "sync", source, "--into", into, "--json"); code == 0 {
		t.Fatalf("a broken manifest must fail the sync: %s", stdout)
	}
	failing = true
	if code, stdout, _ := h.run(t, "sync", source, "--into", into, "--json"); code == 0 || failureCode(t, stdout).Code != "source-fetch" {
		t.Fatalf("an HTTP error is not a reason to fall back: exit %d %s", code, stdout)
	}
	srv.Close()

	var env struct {
		Warnings []string   `json:"warnings"`
		Data     syncResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "sync", source, "--into", into, "--json")), &env); err != nil {
		t.Fatal(err)
	}
	want := "offline, using " + source + " at " + good[:12] + " from the last successful sync just now"
	if res := env.Data; res.Commit != good || !res.Offline || res.LastGoodAt == "" || len(env.Warnings) == 0 || env.Warnings[0] != want {
		t.Fatalf("offline sync falls back to the last good build, not the broken commit: %+v", res)
	}
	if _, stderr := h.mustRunStderr(t, "sync", source, "--into", into, "--ref", good); !strings.Contains(stderr, "offline, using "+source+" at "+good[:12]+", already downloaded") {
		t.Fatalf("an exported commit works offline: %s", stderr)
	}
	for _, args := range [][]string{
		{"sync", source, "--into", into, "--ref", "main"},
		{"sync", srv.URL + "/never.git", "--into", into},
	} {
		code, stdout, _ := h.run(t, append(args, "--json")...)
		if e := failureCode(t, stdout); code == 0 || e.Code != "source-offline" {
			t.Fatalf("%v: nothing synced to fall back to: exit %d %s", args, code, stdout)
		}
	}
}

func TestSyncOfflineKeepsTheInstalledRuntime(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack", "--target", "server")
	h.mustRun(t, "add", "fabric-api")
	into := filepath.Join(t.TempDir(), "server")
	h.mustRun(t, "sync", h.dir, "--into", into)
	_, stderr := h.mustRunStderr(t, "sync", h.dir, "--into", into, "--offline")
	if !strings.Contains(stderr, "! offline, keeping the installed Java runtime java-runtime-epsilon 25.0.1") {
		t.Fatalf("offline runtime refresh: %s", stderr)
	}
}

func TestSyncFromManifestURL(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	files := map[string]bool{"shulker.json": true, "shulker.lock": true}
	down, broken, hits := false, false, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if down {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		if broken && strings.HasSuffix(r.URL.Path, "/shulker.json") {
			w.Write([]byte("{"))
			return
		}
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
		Warnings []string   `json:"warnings"`
		Data     syncResult `json:"data"`
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

	good := env.Data.Sha256

	hits = 0
	if err := json.Unmarshal([]byte(h.mustRun(t, "sync", source, "--into", into, "--offline", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if res := env.Data; !res.Offline || res.Sha256 != good || len(env.Warnings) == 0 || env.Warnings[0] != "--offline, using "+source+" from the last successful sync just now" || hits != 0 {
		t.Fatalf("--offline must not touch the network (%d requests): %+v", hits, res)
	}

	broken = true
	if code, stdout, _ := h.run(t, "sync", source, "--into", into, "--json"); code == 0 {
		t.Fatalf("a broken manifest must fail the sync: %s", stdout)
	}
	broken, down = false, true
	if code, stdout, _ := h.run(t, "sync", source, "--into", into, "--json"); code == 0 || failureCode(t, stdout).Code != "source-fetch" {
		t.Fatalf("an HTTP error is not a reason to fall back: exit %d %s", code, stdout)
	}
	down = false

	files["shulker.lock"] = false
	code, stdout, _ := h.run(t, "sync", source, "--into", into, "--json")
	if code == 0 || failureCode(t, stdout).Code != "source-lock" {
		t.Fatalf("missing lock: exit %d %s", code, stdout)
	}
	code, stdout, _ = h.run(t, "sync", srv.URL+"/pack/other.json", "--into", into, "--json")
	if code == 0 || failureCode(t, stdout).Code != "source-fetch" {
		t.Fatalf("missing manifest: exit %d %s", code, stdout)
	}
	files["shulker.lock"] = true

	srv.Close()
	if err := json.Unmarshal([]byte(h.mustRun(t, "sync", source, "--into", into, "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if res := env.Data; !res.Offline || res.Sha256 != good || len(env.Warnings) == 0 || env.Warnings[0] != "offline, using "+source+" from the last successful sync just now" {
		t.Fatalf("unreachable url falls back to the last good copy: %+v", res)
	}
	code, stdout, _ = h.run(t, "sync", srv.URL+"/other/shulker.json", "--into", into, "--json")
	if code == 0 || failureCode(t, stdout).Code != "source-offline" {
		t.Fatalf("unreachable url with nothing synced: exit %d %s", code, stdout)
	}
}

func TestDiffAndPullInto(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	overrides := filepath.Join(h.dir, "overrides", "config")
	if err := os.MkdirAll(overrides, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overrides, "plain.txt"), []byte("a=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	into := filepath.Join(t.TempDir(), "instance", "minecraft")
	h.mustRun(t, "sync", h.dir, "--into", into)
	if err := os.WriteFile(filepath.Join(into, "config", "plain.txt"), []byte("a=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if stdout := h.mustRun(t, "diff"); !strings.Contains(stdout, "1 file changed in "+into) || !strings.Contains(stdout, "-a=2") {
		t.Fatalf("diff without --into must look at the recorded sync dir: %s", stdout)
	}
	stdout := h.mustRun(t, "diff", "client", "--into", into)
	if !strings.Contains(stdout, "config/plain.txt") || !strings.Contains(stdout, "-a=2") {
		t.Fatalf("diff --into: %s", stdout)
	}

	h.mustRun(t, "pull", "--into", into)
	if data, _ := os.ReadFile(filepath.Join(overrides, "plain.txt")); string(data) != "a=2\n" {
		t.Fatalf("pull --into did not copy the edit back: %q", data)
	}
	if stdout := h.mustRun(t, "diff", "client", "--into", into); !strings.Contains(stdout, "no changes") {
		t.Fatalf("diff after pull: %s", stdout)
	}
}

func TestPullPicksTheDriftedSyncDir(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	overrides := filepath.Join(h.dir, "overrides", "config")
	if err := os.MkdirAll(overrides, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(overrides, "plain.txt"), []byte("a=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "build")
	one := filepath.Join(t.TempDir(), "one")
	two := filepath.Join(t.TempDir(), "two")
	h.mustRun(t, "sync", h.dir, "--into", one)
	h.mustRun(t, "sync", h.dir, "--into", two)
	var lf struct {
		Targets map[string]struct {
			SyncDirs []string `json:"syncDirs"`
		} `json:"targets"`
	}
	h.readJSON(t, "shulker.local.json", &lf)
	if got := strings.Join(lf.Targets["client"].SyncDirs, ","); got != one+","+two {
		t.Fatalf("syncDirs = %s", got)
	}
	if data, _ := os.ReadFile(filepath.Join(h.dir, ".gitignore")); !strings.Contains(string(data), "/shulker.local.json\n") {
		t.Fatalf(".gitignore: %q", data)
	}

	if err := os.WriteFile(filepath.Join(two, "config", "plain.txt"), []byte("a=2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "pull")
	if data, _ := os.ReadFile(filepath.Join(overrides, "plain.txt")); string(data) != "a=2\n" {
		t.Fatalf("pull did not take the edit from the drifted sync dir: %q", data)
	}

	for _, dir := range []string{one, two} {
		if err := os.WriteFile(filepath.Join(dir, "config", "plain.txt"), []byte("a=3\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	code, stdout, _ := h.run(t, "pull", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "ambiguous-into" || strings.Join(e.Candidates, ",") != one+","+two {
		t.Fatalf("pull with two drifted dirs: exit %d %s", code, stdout)
	}

	if err := os.RemoveAll(one); err != nil {
		t.Fatal(err)
	}
	if stdout := h.mustRun(t, "diff"); strings.Contains(stdout, one) || !strings.Contains(stdout, "changed in "+two) {
		t.Fatalf("diff must drop a missing sync dir: %s", stdout)
	}
}

func TestSyncDoesNotFailOnUnwritableLocalFile(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	into := filepath.Join(t.TempDir(), "one")
	h.mustRun(t, "sync", h.dir, "--into", into)

	localPath := filepath.Join(h.dir, "shulker.local.json")
	if err := os.Chmod(localPath, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(localPath, 0o644) })
	if _, stderr := h.mustRunStderr(t, "sync", h.dir, "--into", into); strings.Contains(stderr, "not updated") {
		t.Fatalf("an unchanged local file must not be rewritten: %s", stderr)
	}
	if _, stderr := h.mustRunStderr(t, "build"); strings.Contains(stderr, "not updated") {
		t.Fatalf("an unchanged local file must not be rewritten by build: %s", stderr)
	}

	two := filepath.Join(t.TempDir(), "two")
	if _, stderr := h.mustRunStderr(t, "sync", h.dir, "--into", two); strings.Contains(stderr, "not updated") {
		t.Fatalf("a source project nobody can write to must not warn every sync: %s", stderr)
	}
	if links := readLinks(t, h); len(links) != 2 {
		t.Fatalf("the links registry still knows both directories: %+v", links)
	}
}

func TestSyncFromLocalProjectReadsInstanceDecisions(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	setMod(t, h, "sodium", map[string]any{"feature": "fancy"})
	h.mustRun(t, "feature", "on", "fancy")

	into := filepath.Join(t.TempDir(), "instance")
	if err := os.MkdirAll(into, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(into, "shulker.local.json"), []byte(`{"features":{"fancy":false}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if stdout := h.mustRun(t, "sync", h.dir, "--into", into); !strings.Contains(stdout, "excluded: sodium") {
		t.Fatalf("the instance decision should beat the project one: %s", stdout)
	}
	if st := build.LoadState(into); st.Origin != (build.Origin{Source: h.dir}) {
		t.Fatalf("state origin: %+v", st.Origin)
	}
}

func TestSyncMovesAnOldDataLinkBack(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	into := filepath.Join(t.TempDir(), "instance", "minecraft")
	h.mustRun(t, "sync", h.dir, "--into", into)

	world := filepath.Join(h.dir, build.DataDir, "client", "saves", "New World", "level.dat")
	if err := os.MkdirAll(filepath.Dir(world), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(world, []byte("level"), 0o644); err != nil {
		t.Fatal(err)
	}
	rel, _ := filepath.Rel(into, filepath.Join(h.dir, build.DataDir, "client", "saves"))
	if err := os.Symlink(rel, filepath.Join(into, "saves")); err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(into, build.StateFile)
	var state map[string]any
	if err := json.Unmarshal([]byte(readFile(t, statePath)), &state); err != nil {
		t.Fatal(err)
	}
	state["links"] = []string{"saves"}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(statePath, data, 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := h.mustRun(t, "sync", h.dir, "--into", into)
	if !strings.Contains(stdout, "moved back: saves » "+into) {
		t.Fatalf("sync output: %s", stdout)
	}
	if info, err := os.Lstat(filepath.Join(into, "saves")); err != nil || !info.IsDir() {
		t.Fatalf("saves should be a directory again: %v", err)
	}
	if got := readFile(t, filepath.Join(into, "saves", "New World", "level.dat")); got != "level" {
		t.Fatalf("world data: %q", got)
	}
	if _, err := os.Stat(filepath.Dir(world)); !os.IsNotExist(err) {
		t.Fatalf("the world should have left the project: %v", err)
	}
}

func TestSyncIntoRecoversTheSourceWithoutTheRegistry(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	into := filepath.Join(t.TempDir(), "instance")
	h.mustRun(t, "sync", h.dir, "--into", into)

	if err := os.Remove(registryPath(h)); err != nil {
		t.Fatal(err)
	}
	if stdout := h.mustRun(t, "sync", "--into", into); !strings.Contains(stdout, "unchanged") {
		t.Fatalf("sync --into must rebuild from the recorded source: %s", stdout)
	}
	if links := readLinks(t, h); len(links) != 1 || links[0].Dir != into || links[0].Source != h.dir {
		t.Fatalf("the entry is registered again: %+v", links)
	}

	bare := t.TempDir()
	code, stdout, _ := h.run(t, "sync", "--into", bare, "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "source-unknown" {
		t.Fatalf("a directory with no state: code=%d %s", code, stdout)
	}
}
