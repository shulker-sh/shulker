package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// servePack serves a project's shulker.json and shulker.lock under /pack/, as a raw URL host does.
func servePack(t *testing.T, dir string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/pack/")
		if name != "shulker.json" && name != "shulker.lock" {
			http.NotFound(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dir, name))
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/pack/shulker.json"
}

func rawURLWarnings(warnings []string) []string {
	var found []string
	for _, w := range warnings {
		if strings.Contains(w, "is a raw manifest URL") {
			found = append(found, w)
		}
	}
	return found
}

func TestRawURLSourceWarnsItCarriesNoOverrides(t *testing.T) {
	h := newHarness(t)
	shulkerInstances(t, h)
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	source := servePack(t, h.dir)
	into := filepath.Join(t.TempDir(), "minecraft")

	var env struct {
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "link", "shulker", source, "--as", "raw", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	want := "pack is a raw manifest URL, so its overrides aren't included\nUse its git URL instead, with path for a pack in a subfolder"
	if got := rawURLWarnings(env.Warnings); len(got) != 1 || got[0] != want {
		t.Fatalf("a link to a raw URL warns once: %q", env.Warnings)
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "sync", source, "--into", into, "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if got := rawURLWarnings(env.Warnings); len(got) != 0 {
		t.Fatalf("a sync after the link stays quiet: %q", env.Warnings)
	}

	h.editManifest(t, func(m map[string]any) {
		m["features"] = map[string]any{"shaders": map[string]any{"default": false, "overrides": "shader-overrides"}}
		m["icon"] = "assets/icon.png"
	})
	writeFile(t, filepath.Join(h.dir, "assets", "icon.png"), "png")
	h.mustRun(t, "lock")
	if err := json.Unmarshal([]byte(h.mustRun(t, "link", "shulker", source, "--as", "raw-2", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if got := rawURLWarnings(env.Warnings); len(got) != 1 || !strings.Contains(got[0], "\nNor are the feature overrides shader-overrides and the icon assets/icon.png\n") {
		t.Fatalf("the warning names what the manifest points at: %q", env.Warnings)
	}

	writeFile(t, filepath.Join(h.dir, "extra.jar"), "jar")
	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)["extra"] = map[string]any{"file": "extra.jar"}
	})
	code, stdout, _ := h.run(t, "sync", source, "--into", into, "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "source-incomplete" || !strings.Contains(e.Message, "requires.extra") {
		t.Fatalf("a local file can't arrive from a raw URL: exit %d %s", code, stdout)
	}
}

func TestRawURLModpackWarnsOncePerCommand(t *testing.T) {
	h := newHarness(t)
	pack := t.TempDir()
	writePrismPack(t, pack, "~26.2", `"sodium": {}`, nil)
	source := servePack(t, pack)
	h.mustRun(t, "create", "--loader", "fabric")

	var env struct {
		Warnings []string `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "modpack", "add", source, "--as", "tiny", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if got := rawURLWarnings(env.Warnings); len(got) != 1 {
		t.Fatalf("modpack add warns once: %q", env.Warnings)
	}
	for _, command := range []string{"update", "lock"} {
		if err := json.Unmarshal([]byte(h.mustRun(t, command, "--json")), &env); err != nil {
			t.Fatal(err)
		}
		if got := rawURLWarnings(env.Warnings); len(got) != 0 {
			t.Fatalf("%s after the add stays quiet: %q", command, env.Warnings)
		}
	}
	if err := json.Unmarshal([]byte(h.mustRun(t, "build", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if got := rawURLWarnings(env.Warnings); len(got) != 0 {
		t.Fatalf("a build reads the locked copy and doesn't warn: %q", env.Warnings)
	}

	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	_, gitSource, _ := gitPack(t, "follow", "", "follow.txt")
	if err := json.Unmarshal([]byte(h.mustRun(t, "modpack", "add", gitSource, "--json")), &env); err != nil {
		t.Fatal(err)
	}
	for _, w := range rawURLWarnings(env.Warnings) {
		if strings.Contains(w, gitSource) {
			t.Fatalf("a git source carries its overrides: %q", env.Warnings)
		}
	}
}
