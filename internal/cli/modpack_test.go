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

func writePrismPack(t *testing.T, dir, minecraft, mods string, files map[string]string) {
	t.Helper()
	manifest := `{"name": "` + filepath.Base(dir) + `", "minecraft": "` + minecraft + `", "loader": {"type": "fabric", "version": "*"},
  "requires": {` + mods + `}, "variables": {"greeting": "hello"}, "client": {}}`
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
	Packs map[string]map[string]string `json:"modpacks"`
	Mods  map[string]struct {
		RequiredBy []string `json:"requiredBy"`
	} `json:"mods"`
}

func TestLocalModpack(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	writePrismPack(t, filepath.Join(h.dir, "base"), "~26.2", `"sodium": {}`, map[string]string{
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
	data = []byte(strings.Replace(string(data), `"requires": {}`, `"variables": {"who": "world"}, "requires": {}`, 1))
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}

	stdout := h.mustRun(t, "modpack", "add", "./base")
	if !strings.Contains(stdout, "+ base ") || !strings.Contains(stdout, "+ sodium") || !strings.Contains(stdout, "+ fabric-api") {
		t.Fatalf("modpack add output: %s", stdout)
	}
	l := readLock(t, h)
	if p := l.Packs["base"]; p["source"] != "./base" || len(p["dirSha256"]) != 64 || p["commit"] != "" {
		t.Fatalf("lock modpacks: %v", l.Packs)
	}
	if by := l.Mods["sodium"].RequiredBy; len(by) != 1 || by[0] != "base" {
		t.Fatalf("sodium requiredBy: %v", by)
	}
	var m struct {
		Requires map[string]map[string]string `json:"requires"`
	}
	h.readJSON(t, "shulker.json", &m)
	if len(m.Requires) != 1 || m.Requires["base"]["source"] != "./base" {
		t.Fatalf("manifest requires: %v", m.Requires)
	}
	_, stdout, _ = h.run(t, "modpack", "list", "--json")
	var listEnv struct {
		Data []map[string]any `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &listEnv); err != nil {
		t.Fatalf("modpack list json: %v: %s", err, stdout)
	}
	if listed := listEnv.Data; len(listed) != 1 || listed[0]["key"] != "base" || listed[0]["type"] != "modpack" ||
		listed[0]["kind"] != "local" || listed[0]["state"] != "ok" || listed[0]["version"] != l.Packs["base"]["dirSha256"][:12] {
		t.Fatalf("modpack list: %v", listEnv.Data)
	}

	stdout = h.mustRun(t, "list")
	for _, want := range []string{"Modpacks\n", "• base ./base (local, ", "Mods\n", "• sodium", "fabric-api", "from base"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("list output has no %q: %s", want, stdout)
		}
	}
	if stdout = h.mustRun(t, "list", "--type", "mod"); strings.Contains(stdout, "Modpacks") {
		t.Fatalf("list --type mod should leave modpacks out: %s", stdout)
	}
	code, stdout, _ := h.run(t, "list", "--type", "nope", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error.Code != "usage" {
		t.Fatalf("unknown type: code=%d env=%+v", code, env)
	}
	if code, stdout, _ = h.run(t, "add", "sodium", "--ref", "main", "--json"); code == 0 || !strings.Contains(stdout, "--ref") {
		t.Fatalf("a modpack flag on a mod should fail: %s", stdout)
	}

	code, stdout, _ = h.run(t, "remove", "sodium", "--json")
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error.Code != "modpack-provided" {
		t.Fatalf("remove modpack mod: code=%d env=%+v", code, env)
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
	if stdout = h.mustRun(t, "modpack", "list"); !strings.HasPrefix(stdout, "  Modpacks\n  • base ./base (local, "+l.Packs["base"]["dirSha256"][:12]+", changed)\n") {
		t.Fatalf("modpack list after edit: %s", stdout)
	}
	_, stderr := h.mustRunStderr(t, "build")
	if !strings.Contains(stderr, "modpack base has changed since the lock; run shulker lock") {
		t.Fatalf("drift warning missing: %s", stderr)
	}
	if got := readBuilt(t, h, "config/base.txt"); got != "edited\n" {
		t.Fatalf("base.txt after edit: %q", got)
	}
	stdout, stderr = h.mustRunStderr(t, "lock")
	if !strings.Contains(stdout, "~ base ") || strings.Contains(stderr, "changed since the lock") {
		t.Fatalf("lock after a local modpack edit: %s\n%s", stdout, stderr)
	}
	if stdout = h.mustRun(t, "lock"); !strings.Contains(stdout, "Already up to date") {
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
		t.Fatal("sodium should stay locked while the modpack provides it")
	}

	stdout = h.mustRun(t, "modpack", "remove", "base")
	if !strings.Contains(stdout, "- fabric-api 1.0.0+mc26.2 (was required by sodium)") || !strings.Contains(stdout, "- sodium     1.0.0+mc26.2 (was required by base)") {
		t.Fatalf("modpack remove output: %s", stdout)
	}
	l = readLock(t, h)
	if len(l.Packs) != 0 || len(l.Mods) != 0 {
		t.Fatalf("lock after modpack remove: %+v", l)
	}
	h.mustRun(t, "build")
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "config", "base.txt")); !os.IsNotExist(err) {
		t.Fatalf("modpack file should be removed from the build: %v", err)
	}

	if err := os.WriteFile(filepath.Join(h.dir, "build", "client", ".shulker", "state.json"), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, stderr := h.run(t, "build"); !strings.Contains(stderr, "state.json is unreadable") {
		t.Fatalf("a corrupt state file should warn: %s", stderr)
	}
}

func TestModpackMismatchAndConflict(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	writePrismPack(t, filepath.Join(h.dir, "old"), "~26.1", `"sodium": {}`, nil)
	code, stdout, _ := h.run(t, "modpack", "add", "./old", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error.Code != "modpack-mismatch" || !strings.Contains(env.Error.Message, "~26.1") {
		t.Fatalf("mismatch: code=%d env=%+v", code, env)
	}
	var m struct {
		Requires map[string]any `json:"requires"`
	}
	h.readJSON(t, "shulker.json", &m)
	if len(m.Requires) != 0 {
		t.Fatalf("manifest should be untouched: %v", m.Requires)
	}

	writePrismPack(t, filepath.Join(h.dir, "one"), "~26.2", `"sodium": {}`, nil)
	writePrismPack(t, filepath.Join(h.dir, "two"), "~26.2", `"sodium": {"channel": "beta"}`, nil)
	h.mustRun(t, "add", "./one", "--type", "modpack")
	code, stdout, _ = h.run(t, "modpack", "add", "./two", "--json")
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error.Code != "modpack-conflict" {
		t.Fatalf("conflict: code=%d env=%+v", code, env)
	}
	if code, stdout, _ = h.run(t, "modpack", "add", "./one", "--as", "one", "--json"); code == 0 {
		t.Fatalf("duplicate source should fail: %s", stdout)
	}
}

func TestModpackMissingFromTheLockWarnsOnlyBesideOtherPins(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	writePrismPack(t, filepath.Join(h.dir, "one"), "~26.2", `"sodium": {}`, nil)
	writePrismPack(t, filepath.Join(h.dir, "two"), "~26.2", `"sodium": {}`, nil)
	h.mustRun(t, "modpack", "add", "./one")
	h.mustRun(t, "modpack", "add", "./two")

	lockPath := filepath.Join(h.dir, "shulker.lock")
	var l map[string]any
	h.readJSON(t, "shulker.lock", &l)
	delete(l["modpacks"].(map[string]any), "two")
	data, err := json.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, stderr := h.mustRunStderr(t, "build"); strings.Count(stderr, "modpack two is not in the lock yet") != 1 {
		t.Fatalf("a read without a relock warns once: %s", stderr)
	}
	var env out.Envelope
	_ = json.Unmarshal([]byte(h.mustRun(t, "lock", "--json")), &env)
	if !strings.Contains(strings.Join(env.Warnings, "\n"), "modpack two is not in the lock yet") {
		t.Fatalf("a pack missing from a lock that pins others: %q", env.Warnings)
	}

	if err := os.Remove(lockPath); err != nil {
		t.Fatal(err)
	}
	env = out.Envelope{}
	_ = json.Unmarshal([]byte(h.mustRun(t, "lock", "--json")), &env)
	if len(env.Warnings) != 0 {
		t.Fatalf("a manifest with no lock beside it has nothing to warn about: %q", env.Warnings)
	}
}

func TestModpackSourceMovedUnderTheSameName(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	writePrismPack(t, filepath.Join(h.dir, "base"), "~26.2", `"sodium": {}`, nil)
	h.mustRun(t, "modpack", "add", "./base")
	if err := os.Rename(filepath.Join(h.dir, "base"), filepath.Join(h.dir, "moved")); err != nil {
		t.Fatal(err)
	}
	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["base"].(map[string]any)["source"] = "./moved"
	})
	code, stdout, _ := h.run(t, "build", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code != 0 || !env.LockStale || !strings.Contains(strings.Join(env.Warnings, "\n"), "modpack base has a new source since the lock") {
		t.Fatalf("build after moving the source: code=%d env=%+v", code, env)
	}
	h.mustRun(t, "lock")
	if p := readLock(t, h).Packs["base"]; p["source"] != "./moved" {
		t.Fatalf("lock source after relock: %v", p)
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

func TestGitModpack(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	repo := filepath.Join(t.TempDir(), "shared-pack")
	writePrismPack(t, repo, "^26.1", `"sodium": {}`, map[string]string{"config/git.txt": "v1\n"})
	gitRun(t, repo, "init", "-q", "-b", "main")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "one")
	first := gitRun(t, repo, "rev-parse", "HEAD")
	source := "file://" + repo

	stdout := h.mustRun(t, "modpack", "add", source, "--ref", "main")
	if !strings.Contains(stdout, "+ shared-pack "+first[:12]+" (modpack)") {
		t.Fatalf("modpack add output: %s", stdout)
	}
	if stdout = h.mustRun(t, "modpack", "list"); stdout != "  Modpacks\n  • shared-pack "+source+" (git, ref main, "+first[:7]+")\n" {
		t.Fatalf("modpack list: %s", stdout)
	}
	l := readLock(t, h)
	if p := l.Packs["shared-pack"]; p["commit"] != first || p["ref"] != "main" || p["source"] != source {
		t.Fatalf("lock modpacks: %v", l.Packs)
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
	if !strings.Contains(stdout, "~ shared-pack "+first[:12]+" → "+second[:12]+" (modpack)") {
		t.Fatalf("update output: %s", stdout)
	}
	h.mustRun(t, "build")
	if got := readBuilt(t, h, "config/git.txt"); got != "v2\n" {
		t.Fatalf("git.txt after update: %q", got)
	}

	code, stdout, _ := h.run(t, "modpack", "add", source+"/", "--as", "missing", "--ref", "nope", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error.Code != "modpack-ref" {
		t.Fatalf("bad ref: code=%d env=%+v", code, env)
	}
}

func TestURLModpackAndHandEdits(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tiny.json" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"name": "tiny", "minecraft": "~26.2", "loader": {"type": "fabric", "version": "*"},
  "requires": {"sodium": {}}, "client": {}}`))
	}))
	defer srv.Close()
	source := srv.URL + "/tiny.json"

	stdout := h.mustRun(t, "modpack", "add", source)
	if !strings.Contains(stdout, "+ tiny ") {
		t.Fatalf("modpack add output: %s", stdout)
	}
	l := readLock(t, h)
	if p := l.Packs["tiny"]; p["source"] != source || len(p["sha256"]) != 64 {
		t.Fatalf("lock modpacks: %v", l.Packs)
	}
	h.mustRun(t, "install")
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client", "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatal(err)
	}
	srv.Close()
	h.mustRun(t, "build")

	path := filepath.Join(h.dir, "shulker.json")
	data, _ := os.ReadFile(path)
	edited := strings.Replace(string(data), `"requires": {`, `"requires": {"local": {"source": "./local"}, `, 1)
	if edited == string(data) {
		t.Fatalf("manifest has no requires: %s", data)
	}
	writePrismPack(t, filepath.Join(h.dir, "local"), "~26.2", `"fabric-api": {}`, nil)
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

	h.editManifest(t, func(m map[string]any) { m["requires"] = map[string]any{} })
	stdout = h.mustRun(t, "update")
	if !strings.Contains(stdout, "- tiny ") || !strings.Contains(stdout, "- local ") || !strings.Contains(stdout, "- sodium     1.0.0+mc26.2") {
		t.Fatalf("update after removing modpacks: %s", stdout)
	}
	if l = readLock(t, h); len(l.Packs) != 0 || len(l.Mods) != 0 {
		t.Fatalf("lock after removing modpacks: %+v", l)
	}
}

func renamePack(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, "shulker.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := strings.Replace(string(data), `"name": "`+filepath.Base(dir)+`"`, `"name": "`+name+`"`, 1)
	if edited == string(data) {
		t.Fatalf("pack manifest has no name to replace: %s", data)
	}
	writeFile(t, path, edited)
}

func TestModpackKeyComesFromTheManifestName(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	repo := filepath.Join(h.dir, "mc-pack-v3")
	writePrismPack(t, repo, "~26.2", `"sodium": {}`, nil)
	renamePack(t, repo, "westcoast-smp")

	stdout := h.mustRun(t, "modpack", "add", "./mc-pack-v3")
	if !strings.Contains(stdout, "+ westcoast-smp ") {
		t.Fatalf("modpack add output: %s", stdout)
	}
	var m struct {
		Requires map[string]map[string]string `json:"requires"`
	}
	h.readJSON(t, "shulker.json", &m)
	if len(m.Requires) != 1 || m.Requires["westcoast-smp"]["source"] != "./mc-pack-v3" {
		t.Fatalf("manifest requires: %v", m.Requires)
	}
	if p := readLock(t, h).Packs["westcoast-smp"]; p["source"] != "./mc-pack-v3" {
		t.Fatalf("lock modpacks: %v", readLock(t, h).Packs)
	}
	if by := readLock(t, h).Mods["sodium"].RequiredBy; len(by) != 1 || by[0] != "westcoast-smp" {
		t.Fatalf("sodium requiredBy: %v", by)
	}

	twin := filepath.Join(h.dir, "eastcoast")
	writePrismPack(t, twin, "~26.2", `"sodium": {}`, nil)
	renamePack(t, twin, "westcoast-smp")
	code, stdout, _ := h.run(t, "modpack", "add", "./eastcoast", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "requires-taken" {
		t.Fatalf("two manifests sharing a name should collide: code=%d %s", code, stdout)
	}

	stdout = h.mustRun(t, "modpack", "add", "./eastcoast", "--as", "eastcoast-smp")
	if !strings.Contains(stdout, "+ eastcoast-smp ") {
		t.Fatalf("--as should win over the manifest name: %s", stdout)
	}
	h.readJSON(t, "shulker.json", &m)
	if len(m.Requires) != 2 || m.Requires["eastcoast-smp"]["source"] != "./eastcoast" {
		t.Fatalf("manifest requires after --as: %v", m.Requires)
	}
	if p := readLock(t, h).Packs["eastcoast-smp"]; p["source"] != "./eastcoast" {
		t.Fatalf("lock modpacks after --as: %v", readLock(t, h).Packs)
	}

	code, stdout, _ = h.run(t, "modpack", "add", "./mc-pack-v3", "--as", "again", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "modpack-exists" {
		t.Fatalf("a repeated source should still fail: code=%d %s", code, stdout)
	}
}
