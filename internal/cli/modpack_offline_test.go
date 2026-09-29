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

type servedGitPack struct {
	work, remote, source string
	srv                  *httptest.Server
}

// serveGitPack serves a git modpack over git's dumb HTTP protocol, whose server can be closed to
// make it unreachable.
func serveGitPack(t *testing.T, name, mods string) *servedGitPack {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	g := &servedGitPack{work: filepath.Join(t.TempDir(), name)}
	writePrismPack(t, g.work, "^26.1", mods, map[string]string{"config/" + name + ".txt": "v1\n"})
	gitRun(t, g.work, "init", "-q", "-b", "main")
	gitRun(t, g.work, "add", ".")
	gitRun(t, g.work, "commit", "-q", "-m", "one")
	served := t.TempDir()
	g.remote = filepath.Join(served, name+".git")
	gitRun(t, g.work, "clone", "-q", "--bare", g.work, g.remote)
	gitRun(t, g.remote, "update-server-info")
	g.srv = httptest.NewServer(http.FileServer(http.Dir(served)))
	t.Cleanup(g.srv.Close)
	g.source = g.srv.URL + "/" + name + ".git"
	return g
}

func (g *servedGitPack) head(t *testing.T) string {
	return gitRun(t, g.remote, "rev-parse", "main")
}

func (g *servedGitPack) commit(t *testing.T, name, content string) string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.work, "overrides", "config", name+".txt"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, g.work, "commit", "-q", "-am", "next")
	gitRun(t, g.work, "push", "-q", g.remote, "main")
	gitRun(t, g.remote, "update-server-info")
	return g.head(t)
}

func TestAnUnreachableGitModpackKeepsItsPinWhileTheOthersRefresh(t *testing.T) {
	h := newInPlace(t)
	alpha := serveGitPack(t, "alpha", `"sodium": {}`)
	beta := serveGitPack(t, "beta", `"sodium": {}`)
	h.mustRun(t, "modpack", "add", alpha.source, "--ref", "main")
	h.mustRun(t, "modpack", "add", beta.source, "--ref", "main")
	pinned := alpha.head(t)
	alpha.commit(t, "alpha", "v2\n")
	moved := beta.commit(t, "beta", "v2\n")
	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["fabric-api"] = map[string]any{}
	})
	alpha.srv.Close()

	stdout, stderr := h.mustRunStderr(t, "sync")
	if want := "offline, keeping modpack alpha at " + pinned[:12] + " from the lock"; !strings.Contains(stderr, want) {
		t.Fatalf("%q is missing from\n%s", want, stderr)
	}
	if strings.Contains(stderr, "alpha: offline") {
		t.Fatalf("the warning names its modpack, so it takes no prefix:\n%s", stderr)
	}
	l := readLock(t, h)
	if l.Packs["alpha"]["commit"] != pinned || l.Packs["beta"]["commit"] != moved {
		t.Fatalf("alpha stays at its pin and beta refreshes: %v\n%s", l.Packs, stdout)
	}
	if _, ok := l.Mods["fabric-api"]; !ok {
		t.Fatalf("the project's own mods still relock: %v", l.Mods)
	}
	if got := readFile(t, filepath.Join(h.dir, "config", "alpha.txt")); got != "v1\n" {
		t.Fatalf("alpha.txt: %q", got)
	}
	if got := readFile(t, filepath.Join(h.dir, "config", "beta.txt")); got != "v2\n" {
		t.Fatalf("beta.txt: %q", got)
	}
}

func TestAnUnreachableURLModpackKeepsItsPinnedHash(t *testing.T) {
	h := newInPlace(t)
	manifest := `{"name": "tiny", "minecraft": "~26.2", "loader": {"type": "fabric", "version": "*"}, "requires": {"sodium": {}}, "client": {}}`
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tiny.json" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(manifest))
	}))
	defer srv.Close()
	h.mustRun(t, "modpack", "add", srv.URL+"/tiny.json")
	pinned := readLock(t, h).Packs["tiny"]["sha256"]
	manifest = strings.Replace(manifest, `"sodium": {}`, `"sodium": {}, "fabric-api": {}`, 1)
	srv.Close()

	_, stderr := h.mustRunStderr(t, "sync")
	if want := "offline, keeping modpack tiny at " + pinned[:12] + " from the lock"; !strings.Contains(stderr, want) {
		t.Fatalf("%q is missing from\n%s", want, stderr)
	}
	if got := readLock(t, h).Packs["tiny"]["sha256"]; got != pinned {
		t.Fatalf("tiny moved off its pin: %s", got)
	}
}

func TestAnUnreachableModpackWithNoPinStillFails(t *testing.T) {
	h := newInPlace(t)
	alpha := serveGitPack(t, "alpha", `"sodium": {}`)
	h.mustRun(t, "modpack", "add", alpha.source, "--ref", "main")
	beta := serveGitPack(t, "beta", `"sodium": {}`)
	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["beta"] = map[string]any{"source": beta.source, "ref": "main"}
	})
	beta.srv.Close()

	code, stdout, _ := h.run(t, "--json", "sync")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error == nil || env.Error.Code != "modpack-fetch" || !strings.Contains(env.Error.Message, "couldn't reach "+beta.source) {
		t.Fatalf("a modpack the lock never pinned has nothing to fall back to: exit %d\n%s", code, stdout)
	}
}

func TestAModpackThatAnswersWithAnErrorStillFails(t *testing.T) {
	t.Run("bad ref", func(t *testing.T) {
		h := newInPlace(t)
		alpha := serveGitPack(t, "alpha", `"sodium": {}`)
		h.mustRun(t, "modpack", "add", alpha.source, "--ref", "main")
		h.editManifest(t, func(m map[string]any) {
			m["requires"].(map[string]any)["alpha"].(map[string]any)["ref"] = "nope"
		})
		code, stdout, _ := h.run(t, "--json", "sync")
		var env out.Envelope
		_ = json.Unmarshal([]byte(stdout), &env)
		if code == 0 || env.Error == nil || env.Error.Code != "modpack-ref" {
			t.Fatalf("a missing ref fails hard: exit %d\n%s", code, stdout)
		}
	})
	t.Run("http 404", func(t *testing.T) {
		h := newInPlace(t)
		found := true
		srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !found || r.URL.Path != "/tiny.json" {
				http.NotFound(w, r)
				return
			}
			w.Write([]byte(`{"name": "tiny", "minecraft": "~26.2", "loader": {"type": "fabric", "version": "*"}, "requires": {"sodium": {}}, "client": {}}`))
		}))
		defer srv.Close()
		h.mustRun(t, "modpack", "add", srv.URL+"/tiny.json")
		found = false
		code, stdout, _ := h.run(t, "--json", "sync")
		var env out.Envelope
		_ = json.Unmarshal([]byte(stdout), &env)
		if code == 0 || env.Error == nil || env.Error.Code != "modpack-fetch" {
			t.Fatalf("a 404 fails hard: exit %d\n%s", code, stdout)
		}
	})
}

func TestSyncOfflineInPlaceKeepsEveryModpackAtItsPin(t *testing.T) {
	h := newInPlace(t)
	alpha := serveGitPack(t, "alpha", `"sodium": {}`)
	h.mustRun(t, "modpack", "add", alpha.source, "--ref", "main")
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/tiny.json" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"name": "tiny", "minecraft": "~26.2", "loader": {"type": "fabric", "version": "*"}, "requires": {"sodium": {}}, "client": {}}`))
	}))
	defer srv.Close()
	h.mustRun(t, "modpack", "add", srv.URL+"/tiny.json")
	h.mustRun(t, "sync")
	l := readLock(t, h)

	_, stderr := h.mustRunStderr(t, "sync", "--offline")
	for _, want := range []string{
		"--offline, keeping modpack alpha at " + l.Packs["alpha"]["commit"][:12] + " from the lock",
		"--offline, keeping modpack tiny at " + l.Packs["tiny"]["sha256"][:12] + " from the lock",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("%q is missing from\n%s", want, stderr)
		}
	}
}

func TestUpdateStillFailsOnAnUnreachableModpack(t *testing.T) {
	h := newInPlace(t)
	alpha := serveGitPack(t, "alpha", `"sodium": {}`)
	h.mustRun(t, "modpack", "add", alpha.source, "--ref", "main")
	alpha.srv.Close()

	code, stdout, _ := h.run(t, "--json", "update")
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error == nil || env.Error.Code != "modpack-fetch" {
		t.Fatalf("update asked for new versions, so it fails: exit %d\n%s", code, stdout)
	}
}
