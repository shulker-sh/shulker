package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/out"
)

// playHarness links a shulker instance with its own store root and leaves the harness addressing it
// by nickname, which is how a launch is reached from outside the project directory.
func playHarness(t *testing.T, h *harness, args ...string) (store, gameDir string) {
	t.Helper()
	root := shulkerInstances(t, h)
	store = filepath.Join(t.TempDir(), "store")
	h.mustRun(t, "config", "set", "store", store)
	h.mustRun(t, append([]string{"init", "--yes", "--name", "pack"}, args...)...)
	h.mustRun(t, "link", "shulker")
	h.dir = ""
	return store, filepath.Join(root, "pack")
}

func playJSON(t *testing.T, h *harness, args ...string) playReport {
	t.Helper()
	stdout := h.mustRun(t, append(args, "--json")...)
	var env struct {
		Data playReport `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatal(err)
	}
	return env.Data
}

func TestPlayDryRunFillsTheStore(t *testing.T) {
	h := newHarness(t)
	store, gameDir := playHarness(t, h)

	stdout, stderr := h.mustRunStderr(t, "-i", "pack", "play", "--dry-run")

	for _, want := range []string{"would launch pack", "26.2", "main class: net.minecraft.client.main.Main", "asset index: 26", "classpath: 2 jars"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("play --dry-run: %q is missing from\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "inherits") {
		t.Fatalf("a vanilla instance inherits from nothing: %s", stdout)
	}
	// The game's own arguments carry a session access token, so no part of them is ever printed.
	for _, leak := range []string{"--username", "--accessToken", "auth_access_token", "-cp"} {
		if strings.Contains(stdout+stderr, leak) {
			t.Fatalf("the argv leaked %q into the output:\n%s%s", leak, stdout, stderr)
		}
	}

	assets := sha1Hex([]byte(h.assets["icons/icon_16x16.png"]))
	for _, rel := range []string{
		filepath.Join("versions", "26.2", "26.2.json"),
		filepath.Join("versions", "26.2", "26.2.jar"),
		filepath.Join("libraries", "com", "mojang", "brigadier", "1.3.10", "brigadier-1.3.10.jar"),
		filepath.Join("assets", "indexes", "26.json"),
		filepath.Join("assets", "objects", assets[:2], assets),
		"launcher_profiles.json",
	} {
		if _, err := os.Stat(filepath.Join(store, rel)); err != nil {
			t.Fatalf("the store is missing %s: %v", rel, err)
		}
	}

	rep := playJSON(t, h, "-i", "pack", "play", "--dry-run")
	if rep.Instance != "pack" || rep.Version != "26.2" || rep.Inherits != "" || rep.AssetIndex != "26" {
		t.Fatalf("report %+v", rep)
	}
	if rep.GameDir != gameDir || rep.NativesDir != filepath.Join(gameDir, instance.Dir, "natives") {
		t.Fatalf("directories %+v", rep)
	}
	if rep.Classpath != 2 || rep.ClasspathBytes == 0 || rep.Java == "" {
		t.Fatalf("classpath %+v", rep)
	}

	// Everything is in the store now, so a re-run reaches the network for nothing.
	before := h.storeHits
	h.mustRun(t, "-i", "pack", "play", "--dry-run")
	if h.storeHits != before {
		t.Fatalf("a re-run downloaded %d files", h.storeHits-before)
	}
	if _, err := os.Stat(filepath.Join(store, "loaders.json")); err == nil {
		t.Fatal("a vanilla instance runs no loader installer")
	}
}

func TestPlayDryRunMergesTheLoaderOverVanilla(t *testing.T) {
	h := newHarness(t)
	store, _ := playHarness(t, h, "--loader", "fabric")

	rep := playJSON(t, h, "-i", "pack", "play", "--dry-run")

	if rep.Version != "fabric-loader-0.17.3-26.2" || rep.Inherits != "26.2" {
		t.Fatalf("report %+v", rep)
	}
	if rep.MainClass != "net.fabricmc.loader.impl.launch.knot.KnotClient" {
		t.Fatalf("the loader's main class should win: %+v", rep)
	}
	// The loader's own jar, vanilla's library, and the client jar, in that order.
	if rep.Classpath != 3 {
		t.Fatalf("classpath %+v", rep)
	}
	if _, err := os.Stat(filepath.Join(store, "libraries", "net", "fabricmc", "fabric-loader", "0.17.3", "fabric-loader-0.17.3.jar")); err != nil {
		t.Fatalf("the loader's library is missing from the store: %v", err)
	}
	if _, err := os.Stat(filepath.Join(store, "versions", "26.2", "26.2.json")); err != nil {
		t.Fatalf("the version it inherits from is missing from the store: %v", err)
	}
}

func TestPlayWithoutDryRunSaysItCannotLaunchYet(t *testing.T) {
	h := newHarness(t)
	playHarness(t, h)

	code, stdout, _ := h.run(t, "-i", "pack", "play", "--json")

	if code != out.ExitUsage || !strings.Contains(stdout, "--dry-run") {
		t.Fatalf("exit %d: %s", code, stdout)
	}
}

func TestPlayRefusesAnotherLaunchersInstance(t *testing.T) {
	h := newHarness(t)
	launcherDir := t.TempDir()
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "link", "prism", "--launcher-dir", launcherDir, "--name", "Friends")
	h.dir = ""

	code, stdout, _ := h.run(t, "-i", "friends", "play", "--dry-run", "--json")

	if code == 0 || !strings.Contains(stdout, `"not-shulker"`) {
		t.Fatalf("exit %d: %s", code, stdout)
	}
}
