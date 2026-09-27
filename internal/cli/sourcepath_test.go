package cli

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
)

// monorepo is a git repository holding two locked packs in subfolders and nothing at its root.
func monorepo(t *testing.T, h *harness) (repo, source, commit string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	repo = filepath.Join(t.TempDir(), "packs")
	writePrismPack(t, filepath.Join(repo, "packs", "alpha"), "^26.1", `"sodium": {}`, map[string]string{"config/which.txt": "alpha\n"})
	writePrismPack(t, filepath.Join(repo, "packs", "beta"), "^26.1", "", map[string]string{"config/which.txt": "beta\n"})
	for _, pack := range []string{"alpha", "beta"} {
		h.mustRun(t, "-C", filepath.Join(repo, "packs", pack), "lock")
	}
	gitRun(t, repo, "init", "-q", "-b", "main")
	gitRun(t, repo, "add", ".")
	gitRun(t, repo, "commit", "-q", "-m", "one")
	return repo, "file://" + repo, gitRun(t, repo, "rev-parse", "HEAD")
}

func TestSyncFromGitSubfolder(t *testing.T) {
	h := newHarness(t)
	_, source, commit := monorepo(t, h)
	into := filepath.Join(t.TempDir(), "minecraft")
	var env struct {
		Data syncResult `json:"data"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "sync", source, "--path", "packs/beta", "--into", into, "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if res := env.Data; res.Commit != commit || res.Path != "packs/beta" {
		t.Fatalf("json result: %+v", res)
	}
	if got, _ := os.ReadFile(filepath.Join(into, "config", "which.txt")); string(got) != "beta\n" {
		t.Fatalf("--path packs/beta content: %q", got)
	}
	if st := instance.LoadState(into); st.Origin != (instance.Origin{Source: source, Path: "packs/beta", Commit: commit}) {
		t.Fatalf("state origin: %+v", st.Origin)
	}

	h.mustRun(t, "sync", source, "--path", "packs/alpha", "--into", into)
	if got, _ := os.ReadFile(filepath.Join(into, "config", "which.txt")); string(got) != "alpha\n" {
		t.Fatalf("two packs share one repo: %q", got)
	}

	for _, tc := range []struct {
		args []string
		code string
		says string
	}{
		{[]string{source, "--path", "../escape"}, "source-path", "../escape"},
		{[]string{source, "--path", "/packs/beta"}, "source-path", "/packs/beta"},
		{[]string{source, "--path", "packs/gamma"}, "source-path", commit[:12]},
		{[]string{t.TempDir(), "--path", "packs/beta"}, "source-path", "git"},
	} {
		args := append(append([]string{"sync"}, tc.args...), "--into", into, "--json")
		code, stdout, _ := h.run(t, args...)
		e := failureCode(t, stdout)
		if code == 0 || e.Code != tc.code || !strings.Contains(e.Message, tc.says) {
			t.Fatalf("%v: exit %d %s", tc.args, code, stdout)
		}
	}
}

func TestGitModpackSubfolder(t *testing.T) {
	h := newHarness(t)
	_, source, commit := monorepo(t, h)
	h.mustRun(t, "create", "--loader", "fabric")

	h.mustRun(t, "modpack", "add", source, "--path", "packs/alpha", "--as", "alpha")
	if stdout := h.mustRun(t, "modpack", "list"); stdout != "  Modpacks\n  • alpha "+source+" (git, path packs/alpha, "+commit[:7]+")\n" {
		t.Fatalf("modpack list: %s", stdout)
	}
	var lk map[string]any
	h.readJSON(t, "shulker.lock", &lk)
	if p := lk["modpacks"].(map[string]any)["alpha"].(map[string]any); p["commit"] != commit || p["path"] != "packs/alpha" {
		t.Fatalf("lock modpack: %v", p)
	}
	var m map[string]any
	h.readJSON(t, "shulker.json", &m)
	if entry := m["requires"].(map[string]any)["alpha"].(map[string]any); entry["path"] != "packs/alpha" {
		t.Fatalf("manifest entry: %v", entry)
	}
	h.mustRun(t, "install")
	if got := readBuilt(t, h, "config/which.txt"); got != "alpha\n" {
		t.Fatalf("which.txt: %q", got)
	}

	code, stdout, _ := h.run(t, "modpack", "add", source, "--path", "packs/gamma", "--as", "gamma", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "modpack-manifest" || !strings.Contains(e.Message, "packs/gamma") || !strings.Contains(e.Message, commit[:12]) {
		t.Fatalf("no pack at the path: exit %d %s", code, stdout)
	}
	code, stdout, _ = h.run(t, "modpack", "add", source, "--path", "../up", "--as", "up", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "source-path" {
		t.Fatalf("escaping path: exit %d %s", code, stdout)
	}

	writePrismPack(t, filepath.Join(h.dir, "local"), "^26.1", "", nil)
	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["local"] = map[string]any{"type": "modpack", "source": "./local", "path": "sub"}
	})
	code, stdout, _ = h.run(t, "lock", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "modpack-path" {
		t.Fatalf("path on a local modpack: exit %d %s", code, stdout)
	}

	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["local"] = map[string]any{"type": "modpack", "source": source, "path": "../up"}
	})
	code, stdout, _ = h.run(t, "lock", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "manifest-invalid" || !strings.Contains(e.Message, "requires.local.path") {
		t.Fatalf("escaping path in the manifest: exit %d %s", code, stdout)
	}
}

func TestLinkMojangFromGitSubfolder(t *testing.T) {
	h := newHarness(t)
	_, source, _ := monorepo(t, h)
	launcherDir := t.TempDir()
	var env struct {
		Data linkReport `json:"data"`
	}
	stdout := h.mustRun(t, "link", "mojang", source, "--path", "packs/beta", "--launcher-dir", launcherDir, "--name", "Friends", "--json")
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	if rep := env.Data; rep.Path != "packs/beta" || rep.Modpack != "beta" {
		t.Fatalf("link report: %+v", rep)
	}
	gameDir := filepath.Join(launcherDir, "shulker", "friends")
	if _, entry := onlyModpack(t, instanceManifest(t, gameDir)); entry["source"] != source || entry["path"] != "packs/beta" {
		t.Fatalf("instance modpack entry: %v", entry)
	}
	if got, _ := os.ReadFile(filepath.Join(gameDir, "config", "which.txt")); string(got) != "beta\n" {
		t.Fatalf("which.txt: %q", got)
	}

	r := unlinkJSON(t, h, "Friends", "--launcher", "mojang")
	if r[0].Removed != launcher.RemovedProfile || r[0].Relink != "shulker link mojang "+source+" --path packs/beta --name Friends --launcher-dir "+launcherDir {
		t.Fatalf("unlink mojang: %+v", r[0])
	}
	if stdout := h.mustRun(t, "link", "mojang", source, "--path", "packs/beta", "--launcher-dir", launcherDir, "--name", "Friends"); !strings.Contains(stdout, "follows beta from "+source+", path packs/beta") {
		t.Fatalf("link names the path it follows: %s", stdout)
	}
}

func TestCachePruneKeepsADirectorySyncedFromASubfolder(t *testing.T) {
	h := newHarness(t)
	_, source, _ := monorepo(t, h)
	into := filepath.Join(t.TempDir(), "minecraft")
	h.mustRun(t, "sync", source, "--path", "packs/alpha", "--into", into)
	registry := map[string]any{"$schema": config.RegistrySchemaURL, "instances": []config.Instance{{ID: "alpha", Name: "alpha", Dir: into, Source: source}}}
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, registryPath(h), string(data))

	sodium := (&cache.Cache{Dir: h.cache}).Object(h.jars["sodium"].sha512)
	h.mustRun(t, "--dir", t.TempDir(), "cache", "prune")
	if _, err := os.Stat(sodium); err != nil {
		t.Fatalf("the directory runs on the lock in its checkout's subfolder: %v", err)
	}
	if code, stdout, stderr := h.run(t, "sync", source, "--path", "packs/alpha", "--into", into, "--offline"); code != 0 {
		t.Fatalf("prune keeps the subfolder's offline fallback: %s%s", stdout, stderr)
	}
}
