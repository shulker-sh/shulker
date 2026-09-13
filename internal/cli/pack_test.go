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

	"shulker.sh/shulker/internal/out"
)

func writePack(t *testing.T, dir, minecraft, mods string, files map[string]string) {
	t.Helper()
	manifest := `{"name": "base", "minecraft": "` + minecraft + `", "loader": {"type": "fabric", "version": "*"},
  "targets": {"client": {"side": "client", "overrides": ["overrides"], "build": "build/client"}},
  "mods": {` + mods + `}, "variables": {"greeting": "hello"}}`
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "shulker.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	for rel, content := range files {
		path := filepath.Join(dir, "overrides", rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func readLock(t *testing.T, h *harness) lockView {
	t.Helper()
	var l lockView
	h.readJSON(t, "shulker.lock", &l)
	return l
}

func readBuilt(t *testing.T, h *harness, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.dir, "build", "client", rel))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

type lockView struct {
	Packs map[string]map[string]string `json:"packs"`
	Mods  map[string]struct {
		RequiredBy []string `json:"requiredBy"`
	} `json:"mods"`
}

func TestLocalPack(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes")
	writePack(t, filepath.Join(h.dir, "base"), "~26.2", `"sodium": {}`, map[string]string{
		"config/base.txt":       "from pack\n",
		"config/shared.txt":     "pack\n",
		"config/greet.txt.tmpl": "${greeting} ${who}\n",
	})
	if err := os.MkdirAll(filepath.Join(h.dir, "overrides", "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(h.dir, "overrides", "config", "shared.txt"), []byte("project\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.dir, "shulker.json")
	data, _ := os.ReadFile(path)
	data = []byte(strings.Replace(string(data), `"mods": {}`, `"variables": {"who": "world"}, "mods": {}`, 1))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := h.mustRun(t, "pack", "add", "./base")
	if !strings.Contains(stdout, "+ base ") || !strings.Contains(stdout, "+ sodium") || !strings.Contains(stdout, "+ fabric-api") {
		t.Fatalf("pack add output: %s", stdout)
	}
	l := readLock(t, h)
	if p := l.Packs["./base"]; p["name"] != "base" || len(p["dirSha256"]) != 64 || p["commit"] != "" {
		t.Fatalf("lock packs: %v", l.Packs)
	}
	if by := l.Mods["sodium"].RequiredBy; len(by) != 1 || by[0] != "base" {
		t.Fatalf("sodium requiredBy: %v", by)
	}
	var m struct {
		Packs []map[string]string `json:"packs"`
	}
	h.readJSON(t, "shulker.json", &m)
	if len(m.Packs) != 1 || m.Packs[0]["source"] != "./base" || m.Packs[0]["name"] != "" {
		t.Fatalf("manifest packs: %v", m.Packs)
	}
	_, stdout, _ = h.run(t, "pack", "list", "--json")
	var listEnv struct {
		Data []map[string]string `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &listEnv); err != nil {
		t.Fatalf("pack list json: %v: %s", err, stdout)
	}
	if listed := listEnv.Data; len(listed) != 1 || listed[0]["name"] != "base" || listed[0]["kind"] != "local" || listed[0]["state"] != "ok" || listed[0]["pin"] != l.Packs["./base"]["dirSha256"][:12] {
		t.Fatalf("pack list: %v", listed)
	}

	code, stdout, _ := h.run(t, "remove", "sodium", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error.Code != "pack-provided" {
		t.Fatalf("remove pack mod: code=%d env=%+v", code, env)
	}

	h.mustRun(t, "install")
	if got := readBuilt(t, h, "config/base.txt"); got != "from pack\n" {
		t.Fatalf("base.txt: %q", got)
	}
	if got := readBuilt(t, h, "config/shared.txt"); got != "project\n" {
		t.Fatalf("shared.txt: %q", got)
	}
	if got := readBuilt(t, h, "config/greet.txt"); got != "hello world\n" {
		t.Fatalf("greet.txt: %q", got)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(h.dir, "base", "overrides", "config", "base.txt"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if stdout = h.mustRun(t, "pack", "list"); !strings.HasPrefix(stdout, "  • base ./base (local, changed, pinned "+l.Packs["./base"]["dirSha256"][:12]+")\n") {
		t.Fatalf("pack list after edit: %s", stdout)
	}
	_, stderr := h.mustRunStderr(t, "build")
	if !strings.Contains(stderr, "pack base has changed since the lock; run shulker lock") {
		t.Fatalf("drift warning missing: %s", stderr)
	}
	if got := readBuilt(t, h, "config/base.txt"); got != "edited\n" {
		t.Fatalf("base.txt after edit: %q", got)
	}
	stdout, stderr = h.mustRunStderr(t, "lock")
	if !strings.Contains(stdout, "~ base ") || strings.Contains(stderr, "changed since the lock") {
		t.Fatalf("lock after a local pack edit: %s\n%s", stdout, stderr)
	}
	if stdout = h.mustRun(t, "lock"); !strings.Contains(stdout, "already up to date") {
		t.Fatalf("second lock: %s", stdout)
	}

	h.mustRun(t, "add", "sodium")
	l = readLock(t, h)
	if by := l.Mods["sodium"].RequiredBy; len(by) != 1 || by[0] != "base" {
		t.Fatalf("sodium requiredBy after add: %v", by)
	}
	h.mustRun(t, "remove", "sodium")
	l = readLock(t, h)
	if _, ok := l.Mods["sodium"]; !ok {
		t.Fatal("sodium should stay locked while the pack provides it")
	}

	stdout = h.mustRun(t, "pack", "remove", "base")
	if !strings.Contains(stdout, "- fabric-api (was required by sodium)") || !strings.Contains(stdout, "- sodium (was required by base)") {
		t.Fatalf("pack remove output: %s", stdout)
	}
	l = readLock(t, h)
	if len(l.Packs) != 0 || len(l.Mods) != 0 {
		t.Fatalf("lock after pack remove: %+v", l)
	}
	h.mustRun(t, "build")
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "config", "base.txt")); !os.IsNotExist(err) {
		t.Fatalf("pack file should be removed from the build: %v", err)
	}

	if err := os.WriteFile(filepath.Join(h.dir, "build", "client", ".shulker-state.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, stderr := h.run(t, "build"); !strings.Contains(stderr, ".shulker-state.json is unreadable") {
		t.Fatalf("a corrupt state file should warn: %s", stderr)
	}
}

func TestPackMismatchAndConflict(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes")
	writePack(t, filepath.Join(h.dir, "old"), "~26.1", `"sodium": {}`, nil)
	code, stdout, _ := h.run(t, "pack", "add", "./old", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error.Code != "pack-mismatch" || !strings.Contains(env.Error.Message, "~26.1") {
		t.Fatalf("mismatch: code=%d env=%+v", code, env)
	}
	var m struct {
		Packs []map[string]string `json:"packs"`
	}
	h.readJSON(t, "shulker.json", &m)
	if len(m.Packs) != 0 {
		t.Fatalf("manifest should be untouched: %v", m.Packs)
	}

	writePack(t, filepath.Join(h.dir, "one"), "~26.2", `"sodium": {}`, nil)
	writePack(t, filepath.Join(h.dir, "two"), "~26.2", `"sodium": {"channel": "beta"}`, nil)
	h.mustRun(t, "pack", "add", "./one")
	code, stdout, _ = h.run(t, "pack", "add", "./two", "--json")
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error.Code != "pack-conflict" {
		t.Fatalf("conflict: code=%d env=%+v", code, env)
	}
	if code, stdout, _ = h.run(t, "pack", "add", "./one", "--name", "one", "--json"); code == 0 {
		t.Fatalf("duplicate source should fail: %s", stdout)
	}
}

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	cmd.Dir = dir
	outb, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, outb)
	}
	return strings.TrimSpace(string(outb))
}

func TestGitPack(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := newHarness(t)
	h.mustRun(t, "init", "--yes")
	repo := filepath.Join(t.TempDir(), "shared-pack")
	writePack(t, repo, "^26.1", `"sodium": {}`, map[string]string{"config/git.txt": "v1\n"})
	gitRun(t, repo, "init", "-q", "-b", "main")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "one")
	first := gitRun(t, repo, "rev-parse", "HEAD")
	source := "file://" + repo

	stdout := h.mustRun(t, "pack", "add", source, "--ref", "main")
	if !strings.Contains(stdout, "+ shared-pack "+first[:12]+" (pack)") {
		t.Fatalf("pack add output: %s", stdout)
	}
	if stdout = h.mustRun(t, "pack", "list"); stdout != "  • shared-pack "+source+" (git, ok, pinned "+first[:12]+", ref main)\n" {
		t.Fatalf("pack list: %s", stdout)
	}
	l := readLock(t, h)
	if p := l.Packs[source]; p["commit"] != first || p["ref"] != "main" || p["name"] != "shared-pack" {
		t.Fatalf("lock packs: %v", l.Packs)
	}
	h.mustRun(t, "install")
	if got := readBuilt(t, h, "config/git.txt"); got != "v1\n" {
		t.Fatalf("git.txt: %q", got)
	}
	if _, err := os.Stat(filepath.Join(h.cache, "packs", "src", first, "shulker.json")); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(repo, "overrides", "config", "git.txt"), []byte("v2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, repo, "commit", "-q", "-am", "two")
	second := gitRun(t, repo, "rev-parse", "HEAD")
	h.mustRun(t, "build")
	if got := readBuilt(t, h, "config/git.txt"); got != "v1\n" {
		t.Fatalf("build must stay on the locked commit: %q", got)
	}
	stdout = h.mustRun(t, "update", "shared-pack")
	if !strings.Contains(stdout, "~ shared-pack "+first[:12]+" ⟶ "+second[:12]+" (pack)") {
		t.Fatalf("update output: %s", stdout)
	}
	h.mustRun(t, "build")
	if got := readBuilt(t, h, "config/git.txt"); got != "v2\n" {
		t.Fatalf("git.txt after update: %q", got)
	}

	code, stdout, _ := h.run(t, "pack", "add", source+"/", "--name", "missing", "--ref", "nope", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error.Code != "pack-ref" {
		t.Fatalf("bad ref: code=%d env=%+v", code, env)
	}
}

func TestURLPackAndHandEdits(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tiny.json" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"name": "tiny", "minecraft": "~26.2", "loader": {"type": "fabric", "version": "*"},
  "targets": {"client": {"side": "client", "overrides": ["overrides"]}}, "mods": {"sodium": {}}}`))
	}))
	defer srv.Close()
	source := srv.URL + "/tiny.json"

	stdout := h.mustRun(t, "pack", "add", source)
	if !strings.Contains(stdout, "+ tiny ") {
		t.Fatalf("pack add output: %s", stdout)
	}
	l := readLock(t, h)
	if p := l.Packs[source]; p["name"] != "tiny" || len(p["sha256"]) != 64 {
		t.Fatalf("lock packs: %v", l.Packs)
	}
	h.mustRun(t, "install")
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	h.mustRun(t, "build")

	path := filepath.Join(h.dir, "shulker.json")
	data, _ := os.ReadFile(path)
	edited := strings.Replace(string(data), `"packs": [`, `"packs": [{"source": "./local"}, `, 1)
	if edited == string(data) {
		t.Fatalf("manifest has no packs array: %s", data)
	}
	writePack(t, filepath.Join(h.dir, "local"), "~26.2", `"fabric-api": {}`, nil)
	if err := os.WriteFile(path, []byte(edited), 0o644); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := h.run(t, "build", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code != 0 || !env.LockStale {
		t.Fatalf("stale build: code=%d env=%+v", code, env)
	}
	stdout = h.mustRun(t, "update", "fabric-api")
	if !strings.Contains(stdout, "+ local ") {
		t.Fatalf("update after hand edit: %s", stdout)
	}
	l = readLock(t, h)
	if by := l.Mods["fabric-api"].RequiredBy; len(by) != 2 || by[0] != "local" || by[1] != "sodium" {
		t.Fatalf("fabric-api requiredBy: %v", by)
	}

	data, _ = os.ReadFile(path)
	stripped := strings.Replace(string(data), `{"source": "./local"}, `, ``, 1)
	stripped = stripped[:strings.Index(stripped, `"packs"`)] + `"packs": [],` + stripped[strings.Index(stripped, `"mods"`):]
	if err := os.WriteFile(path, []byte(stripped), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout = h.mustRun(t, "update")
	if !strings.Contains(stdout, "- tiny (pack)") || !strings.Contains(stdout, "- local (pack)") || !strings.Contains(stdout, "- sodium") {
		t.Fatalf("update after removing packs: %s", stdout)
	}
	if l = readLock(t, h); len(l.Packs) != 0 || len(l.Mods) != 0 {
		t.Fatalf("lock after removing packs: %+v", l)
	}
}
