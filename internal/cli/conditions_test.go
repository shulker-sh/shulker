package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
)

func setMod(t *testing.T, h *harness, id string, entry map[string]any) {
	t.Helper()
	h.editManifest(t, func(m map[string]any) {
		m["requires"].(map[string]any)[id] = entry
	})
}

func setFeatures(t *testing.T, h *harness, features []string) {
	t.Helper()
	h.editManifest(t, func(m map[string]any) {
		target := m["targets"].(map[string]any)["client"].(map[string]any)
		if features == nil {
			delete(target, "features")
			return
		}
		target["features"] = features
	})
}

func modsDir(t *testing.T, h *harness) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(h.dir, "build", "client", "mods"))
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jar") && e.Name() != "shulker-pack.jar" {
			names = append(names, e.Name())
		}
	}
	return names
}

func TestFeatureConditionsFilterModsAndDependencies(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")

	setMod(t, h, "sodium", map[string]any{"feature": "fancy"})
	stdout := h.mustRun(t, "build")
	if !strings.Contains(stdout, "2 excluded") || !strings.Contains(stdout, "excluded: sodium (needs feature fancy)\n") || !strings.Contains(stdout, "excluded: fabric-api (only required by sodium)\n") {
		t.Fatalf("gated build: %s", stdout)
	}
	if jars := modsDir(t, h); len(jars) != 0 {
		t.Fatalf("mods dir should be empty, got %v", jars)
	}

	setFeatures(t, h, []string{"fancy"})
	stdout = h.mustRun(t, "build")
	if strings.Contains(stdout, "excluded") || len(modsDir(t, h)) != 2 {
		t.Fatalf("feature on by target default: %s %v", stdout, modsDir(t, h))
	}

	setMod(t, h, "sodium", map[string]any{"feature": []string{"fancy", "!shaders"}})
	setFeatures(t, h, []string{"fancy", "shaders"})
	stdout = h.mustRun(t, "build")
	if !strings.Contains(stdout, "excluded: sodium (feature shaders is on)\n") || len(modsDir(t, h)) != 0 {
		t.Fatalf("negated feature: %s", stdout)
	}

	setMod(t, h, "sodium", map[string]any{"feature": "!shaders"})
	setFeatures(t, h, nil)
	stdout = h.mustRun(t, "build")
	if strings.Contains(stdout, "excluded") || len(modsDir(t, h)) != 2 {
		t.Fatalf("negation alone ships by default: %s", stdout)
	}
}

func TestOSConditionsUseTheBuildMachine(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	here := build.DetectOS()
	other := "windows"
	if here == other {
		other = "linux"
	}

	setMod(t, h, "sodium", map[string]any{"os": here})
	if stdout := h.mustRun(t, "build"); strings.Contains(stdout, "excluded") {
		t.Fatalf("matching os: %s", stdout)
	}
	setMod(t, h, "sodium", map[string]any{"os": []string{other, "!" + here}})
	if stdout := h.mustRun(t, "build"); !strings.Contains(stdout, "excluded: sodium (os is "+here+")\n") {
		t.Fatalf("negated os: %s", stdout)
	}
	setMod(t, h, "sodium", map[string]any{"os": other})
	if stdout := h.mustRun(t, "build"); !strings.Contains(stdout, "excluded: sodium (needs os "+other+")\n") {
		t.Fatalf("other os: %s", stdout)
	}
}

func TestGatedDependencyStillShipsWhenRequired(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium", "fabric-api")

	setMod(t, h, "fabric-api", map[string]any{"feature": "api"})
	stdout, stderr := h.mustRunStderr(t, "build")
	if strings.Contains(stdout, "excluded") || len(modsDir(t, h)) != 2 {
		t.Fatalf("required dependency dropped: %s", stdout)
	}
	if !strings.Contains(stderr, "fabric-api is gated off (needs feature api) but sodium requires it; included") {
		t.Fatalf("expected a warning, got: %s", stderr)
	}
}

func TestExportMrpackLeavesOutOSGatedMods(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.allowMrpackHost(t)
	setMod(t, h, "sodium", map[string]any{"os": "macos"})

	_, stderr := h.mustRunStderr(t, "export", "mrpack", "--version", "0.1")
	if !strings.Contains(stderr, "sodium (needs os macos): left out of client; pass --os to export that variation") {
		t.Fatalf("expected an os warning, got: %s", stderr)
	}
	index, _ := readMrpack(t, filepath.Join(h.dir, "build", "pack-0.1.mrpack"))
	if len(index.Files) != 0 {
		t.Fatalf("expected no mods without --os, got %+v", index.Files)
	}

	_, stderr = h.mustRunStderr(t, "export", "mrpack", "--version", "0.2", "--os", "macos")
	if strings.Contains(stderr, "left out") {
		t.Fatalf("unexpected warning: %s", stderr)
	}
	index, _ = readMrpack(t, filepath.Join(h.dir, "build", "pack-0.2.mrpack"))
	if len(index.Files) != 2 {
		t.Fatalf("expected sodium and fabric-api with --os macos, got %+v", index.Files)
	}
}

func osLabel() string {
	switch runtime.GOOS {
	case "darwin":
		return "macOS"
	case "windows":
		return "Windows"
	case "linux":
		return "Linux"
	}
	return runtime.GOOS
}

func markerDescription(t *testing.T, h *harness) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(h.dir, "build", "client", "mods", "shulker-pack.jar"))
	if err != nil {
		t.Fatal(err)
	}
	var meta struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(readZip(t, data)["fabric.mod.json"], &meta); err != nil {
		t.Fatal(err)
	}
	return meta.Description
}

func TestMarkerDescribesTheVariation(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	h.editManifest(t, func(m map[string]any) {
		m["description"] = "Fast <3 and <b>plain</b>."
	})
	setMod(t, h, "sodium", map[string]any{"os": []string{"!windows", "!linux", "!macos"}, "feature": "!shaders"})
	setFeatures(t, h, []string{"fancy"})
	h.mustRun(t, "install")

	desc := markerDescription(t, h)
	want := "Fast \\<3 and \\<b>plain\\</b>.\n\nMinecraft 26.2 \u2022 fabric 0.17.3 \u2022 0 mods\n<gray><bold>OS:</bold></gray> " + osLabel() + " \u2022 <gray><bold>Features:</bold></gray> fancy"
	if desc != want {
		t.Fatalf("excluded build description:\n%s", desc)
	}

	var others []string
	for _, name := range []string{"windows", "linux", "macos"} {
		if name != build.DetectOS() {
			others = append(others, "!"+name)
		}
	}
	setMod(t, h, "sodium", map[string]any{"os": append(others, build.DetectOS()), "feature": "!shaders"})
	setFeatures(t, h, nil)
	h.mustRun(t, "build")
	desc = markerDescription(t, h)
	if !strings.Contains(desc, "\u2022 2 mods\n<gray><bold>OS:</bold></gray> "+osLabel()+"\n\n<bold>Mods</bold>\n  \u2022 sodium <gray>("+osLabel()+")</gray>\n\n<bold>Dependencies</bold>\n  \u2022 fabric-api") {
		t.Fatalf("os-gated build description:\n%s", desc)
	}

	setMod(t, h, "sodium", map[string]any{"os": build.DetectOS(), "feature": []string{"fancy", "shaders", "!potato"}})
	setFeatures(t, h, []string{"fancy", "shaders"})
	h.mustRun(t, "build")
	if desc = markerDescription(t, h); !strings.Contains(desc, "  \u2022 sodium <gray>("+osLabel()+", features: fancy, shaders)</gray>\n") {
		t.Fatalf("feature-gated build description:\n%s", desc)
	}

	setMod(t, h, "sodium", map[string]any{"feature": "fancy"})
	setFeatures(t, h, []string{"fancy"})
	h.mustRun(t, "build")
	if desc = markerDescription(t, h); strings.Contains(desc, "OS:") || !strings.Contains(desc, "\u2022 2 mods\n<gray><bold>Features:</bold></gray> fancy\n\n<bold>Mods</bold>\n  \u2022 sodium <gray>(feature: fancy)</gray>\n") {
		t.Fatalf("universal build description:\n%s", desc)
	}
}

func TestLockShowsWhereGatedModsLand(t *testing.T) {
	other := "windows"
	if build.DetectOS() == other {
		other = "linux"
	}

	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	setMod(t, h, "sodium", map[string]any{"feature": "fancy", "os": other})
	stdout := h.mustRun(t, "lock")
	if !strings.Contains(stdout, "» no targets (") || !strings.Contains(stdout, "os: "+other+", feature: fancy, off in every target)") {
		t.Fatalf("feature off in every target, os reported not checked: %s", stdout)
	}
	if !strings.Contains(stdout, "» no targets (required by sodium)") {
		t.Fatalf("a dependency lands where its requirer does: %s", stdout)
	}

	h = newHarness(t)
	h.mustRun(t, "init", "--yes", "--name", "pack")
	setFeatures(t, h, []string{"fancy"})
	setMod(t, h, "sodium", map[string]any{"feature": []string{"fancy", "!lowend"}})
	stdout = h.mustRun(t, "lock")
	if !strings.Contains(stdout, "» all targets (") || !strings.Contains(stdout, "feature: fancy and not lowend)") || strings.Contains(stdout, "off in every target") {
		t.Fatalf("feature on by target default: %s", stdout)
	}
}
