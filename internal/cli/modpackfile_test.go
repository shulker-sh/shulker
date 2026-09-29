package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

// packWithLocalFiles locks localFiles' project, moves it to dir as a modpack named base, and leaves
// a fresh project in its place with an empty cache, so nothing the pack locked is cached yet.
func packWithLocalFiles(t *testing.T, dir string) (*harness, fakeJar) {
	t.Helper()
	h, jar := localFiles(t)
	h.editManifest(t, func(m map[string]any) { m["name"] = "base" })
	h.mustRun(t, "lock")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"shulker.json", "shulker.lock", "files"} {
		if err := os.Rename(filepath.Join(h.dir, name), filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
	h.mustRun(t, "create", "--loader", "fabric")
	if err := os.RemoveAll(h.cache); err != nil {
		t.Fatal(err)
	}
	return h, jar
}

func assertPackFilesBuilt(t *testing.T, h *harness, jar fakeJar) {
	t.Helper()
	if got := readBuilt(t, h, "mods/private-mod-1.4.jar"); got != string(jar.data) {
		t.Fatal("the modpack's local mod is placed")
	}
	for _, rel := range []string{"resourcepacks/faithful.zip", "shaderpacks/bsl.zip"} {
		if readBuilt(t, h, rel) == "" {
			t.Errorf("%s is not placed", rel)
		}
	}
}

func installJSON(t *testing.T, h *harness) out.Envelope {
	t.Helper()
	var env out.Envelope
	if err := json.Unmarshal([]byte(h.mustRun(t, "install", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	return env
}

func TestLocalModpackFileEntriesLockAndBuild(t *testing.T) {
	for _, unlocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "locked", true: "unlocked"}[unlocked], func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "base")
			h, jar := packWithLocalFiles(t, dir)
			args := []string{"modpack", "add", dir}
			if unlocked {
				args = append(args, "--unlocked")
			}
			h.mustRun(t, args...)

			l := h.readLock(t)
			mod := l.Mods["private-mod"]
			if mod.File != "files/private-mod-1.4.jar" || mod.Sha512 != jar.sha512 {
				t.Fatalf("the modpack's local mod locks by its bytes: %+v", mod)
			}
			if _, err := os.Stat(filepath.Join(h.cache, "objects", jar.sha512[:2], jar.sha512)); err != nil {
				t.Fatal("locking hashes the modpack's file into the cache")
			}
			if !unlocked && (l.ResourcePacks["faithful"].File != "files/faithful.zip" || l.Shaders["bsl"].Modpack != "base") {
				t.Fatalf("the locked modpack's local packs: %+v %+v", l.ResourcePacks, l.Shaders)
			}
			if env := installJSON(t, h); env.LockStale || len(env.Warnings) != 0 {
				t.Fatalf("install: %+v", env)
			}
			if _, err := os.Stat(filepath.Join(h.dir, "files")); !os.IsNotExist(err) {
				t.Fatal("nothing is copied into the consuming project")
			}
			if got := readBuilt(t, h, "mods/private-mod-1.4.jar"); got != string(jar.data) {
				t.Fatal("the modpack's local mod is placed")
			}
			if !unlocked {
				assertPackFilesBuilt(t, h, jar)
			}

			changed := makeJarWith(t, "private-mod", "private-mod-1.4.jar", "client", `"depends":{"fabricloader":">=0.17"}`)
			if err := os.WriteFile(filepath.Join(dir, "files", "private-mod-1.4.jar"), changed.data, 0o644); err != nil {
				t.Fatal(err)
			}
			if env := installJSON(t, h); len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "modpack base has changed since the lock") {
				t.Fatalf("a changed file in the modpack: %+v", env)
			}
			if err := os.WriteFile(filepath.Join(dir, "files", "private-mod-1.4.jar"), jar.data, 0o644); err != nil {
				t.Fatal(err)
			}

			if err := os.RemoveAll(h.cache); err != nil {
				t.Fatal(err)
			}
			if env := installJSON(t, h); len(env.Warnings) != 0 {
				t.Fatalf("install refills the cache from the modpack's directory: %+v", env)
			}
			if got := readBuilt(t, h, "mods/private-mod-1.4.jar"); got != string(jar.data) {
				t.Fatal("the modpack's local mod is placed after the cache is emptied")
			}
		})
	}
}

func TestGitModpackFileEntries(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := filepath.Join(t.TempDir(), "base")
	h, jar := packWithLocalFiles(t, repo)
	gitRun(t, repo, "init", "-q", "-b", "main")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "one")

	h.mustRun(t, "modpack", "add", "file://"+repo)
	if l := h.readLock(t); l.Mods["private-mod"].Sha512 != jar.sha512 || l.Mods["private-mod"].Modpack != "base" {
		t.Fatalf("the git modpack's local mod: %+v", l.Mods["private-mod"])
	}
	if env := installJSON(t, h); env.LockStale || len(env.Warnings) != 0 {
		t.Fatalf("install: %+v", env)
	}
	assertPackFilesBuilt(t, h, jar)
}

func TestGitModpackUncommittedFile(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo := filepath.Join(t.TempDir(), "base")
	h, jar := packWithLocalFiles(t, repo)
	gitRun(t, repo, "init", "-q", "-b", "main")
	gitRun(t, repo, "add", "shulker.json", "shulker.lock", "files/faithful.zip", "files/bsl.zip", "files/iris-1.9.jar")
	gitRun(t, repo, "commit", "-q", "-m", "one")

	code, stdout, _ := h.run(t, "modpack", "add", "file://"+repo, "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "local-file-missing" || !strings.Contains(e.Message, "files/private-mod-1.4.jar") {
		t.Fatalf("an uncommitted file the cache lacks: code=%d %s", code, stdout)
	}

	cacheLocalCopy(t, h, jar)
	var env out.Envelope
	if err := json.Unmarshal([]byte(h.mustRun(t, "modpack", "add", "file://"+repo, "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if len(env.Warnings) != 1 || !strings.Contains(env.Warnings[0], "files/private-mod-1.4.jar is gone; using the copy in the cache") {
		t.Fatalf("an uncommitted file the cache has warns once: %+v", env)
	}
	if env := installJSON(t, h); env.LockStale || len(env.Warnings) != 0 {
		t.Fatalf("install: %+v", env)
	}
	assertPackFilesBuilt(t, h, jar)

	if err := os.RemoveAll(filepath.Join(h.cache, "objects")); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ = h.run(t, "install", "--json")
	env = out.Envelope{}
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error == nil || env.Error.Code != "missing-files" || len(env.Error.Items) != 1 || !strings.Contains(env.Error.Items[0], "private-mod") {
		t.Fatalf("install with neither file nor cache: code=%d env=%+v", code, env)
	}
}

// cacheLocalCopy puts jar's bytes in the cache the way locking it in its own project would.
func cacheLocalCopy(t *testing.T, h *harness, jar fakeJar) {
	t.Helper()
	writeProjectFile(t, h, "files/"+jar.filename, jar.data)
	h.editManifest(t, func(m map[string]any) {
		m["requires"] = map[string]any{"private-mod": map[string]any{"file": "files/" + jar.filename}}
	})
	h.mustRun(t, "lock")
	h.editManifest(t, func(m map[string]any) { m["requires"] = map[string]any{} })
	h.mustRun(t, "lock")
	if err := os.RemoveAll(filepath.Join(h.dir, "files")); err != nil {
		t.Fatal(err)
	}
}

func TestURLModpackFileEntryFails(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tiny.json" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"name": "tiny", "minecraft": "~26.2", "loader": {"type": "fabric", "version": "*"},
  "requires": {"faithful": {"type": "resourcepack", "file": "files/faithful.zip"}}, "client": {}}`))
	}))
	defer srv.Close()

	code, stdout, _ := h.run(t, "modpack", "add", srv.URL+"/tiny.json", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "modpack-url-file" || !strings.Contains(e.Message, "faithful") {
		t.Fatalf("a URL modpack with a file entry: code=%d %s", code, stdout)
	}
}

func TestNestedModpackFileServedOnlyFromCache(t *testing.T) {
	root := t.TempDir()
	inner, outer := filepath.Join(root, "base"), filepath.Join(root, "outer")
	h, jar := packWithLocalFiles(t, inner)
	h.editManifest(t, func(m map[string]any) { m["name"] = "outer" })
	h.mustRun(t, "modpack", "add", inner)
	if err := os.MkdirAll(outer, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"shulker.json", "shulker.lock"} {
		if err := os.Rename(filepath.Join(h.dir, name), filepath.Join(outer, name)); err != nil {
			t.Fatal(err)
		}
	}
	h.mustRun(t, "create", "--loader", "fabric")
	h.mustRun(t, "modpack", "add", outer)
	if l := h.readLock(t); l.Mods["private-mod"].Sha512 != jar.sha512 || l.Mods["private-mod"].Modpack != "outer" {
		t.Fatalf("the nested modpack's local mod: %+v", l.Mods["private-mod"])
	}

	other := makeJarWith(t, "private-mod", "private-mod-1.4.jar", "client", `"depends":{"fabricloader":">=0.17"}`)
	writeFile(t, filepath.Join(outer, "files", "private-mod-1.4.jar"), string(other.data))
	if err := os.RemoveAll(filepath.Join(h.cache, "objects")); err != nil {
		t.Fatal(err)
	}
	code, stdout, _ := h.run(t, "install", "--json")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error == nil || env.Error.Code != "missing-files" {
		t.Fatalf("install with the nested files uncached: code=%d env=%+v", code, env)
	}
	var want []string
	for _, entry := range []string{"iris: files/iris-1.9.jar", "private-mod: files/private-mod-1.4.jar", "faithful: files/faithful.zip", "bsl: files/bsl.zip"} {
		want = append(want, entry+" comes from modpack base inside modpack outer, so only the cache can serve it, and the cache has no copy of it")
	}
	if !reflect.DeepEqual(env.Error.Items, want) {
		t.Fatalf("the nested files' problems:\n got %q\nwant %q", env.Error.Items, want)
	}
}
