package cli

import (
	"os"
	"path/filepath"
	"testing"
)

type platformLockView struct {
	Minecraft string `json:"minecraft"`
	Loader    struct {
		Type    string `json:"type"`
		Version string `json:"version"`
	} `json:"loader"`
}

// dropPlatform takes minecraft and loader out of shulker.json, leaving the project
// to inherit both from its locked modpacks.
func dropPlatform(t *testing.T, h *harness) {
	t.Helper()
	h.editManifest(t, func(m map[string]any) {
		delete(m, "minecraft")
		delete(m, "loader")
	})
}

func removeLock(t *testing.T, h *harness) {
	t.Helper()
	if err := os.Remove(filepath.Join(h.dir, "shulker.lock")); err != nil {
		t.Fatal(err)
	}
}

func TestAProjectWithNoPlatformInheritsItFromALockedModpack(t *testing.T) {
	h, _ := projectWithLockedPack(t, "base")
	h.mustRun(t, "modpack", "add", "./base")
	var want platformLockView
	h.readJSON(t, filepath.Join("base", "shulker.lock"), &want)

	dropPlatform(t, h)
	removeLock(t, h)
	h.mustRun(t, "lock")

	var got platformLockView
	h.readJSON(t, "shulker.lock", &got)
	if got.Minecraft != want.Minecraft || got.Loader != want.Loader {
		t.Fatalf("the platform should come from the locked modpack (%+v), got %+v", want, got)
	}
}

// The relock reconciles before the command body runs, so the platform has to survive
// a pass with nothing to inherit from yet: the modpack being added is what supplies it.
func TestModpackAddSuppliesAPlatformTheManifestDoesNotSet(t *testing.T) {
	h, _ := projectWithLockedPack(t, "base")
	dropPlatform(t, h)

	h.mustRun(t, "modpack", "add", "./base")

	var got platformLockView
	h.readJSON(t, "shulker.lock", &got)
	if got.Minecraft == "" || got.Loader.Type != "fabric" {
		t.Fatalf("the inherited platform should still be in the lock, got %+v", got)
	}
}

func TestLockedModpacksDisagreeingAboutThePlatform(t *testing.T) {
	h, base := projectWithLockedPack(t, "base")
	other := filepath.Join(h.dir, "other")
	writePrismPack(t, other, "~26.2", `"sodium": {}`, nil)
	copyLock(t, base, other)
	editPackLock(t, other, func(m map[string]any) { m["minecraft"] = "26.1" })
	h.editManifest(t, func(m map[string]any) {
		requires, _ := m["requires"].(map[string]any)
		if requires == nil {
			requires = map[string]any{}
			m["requires"] = requires
		}
		requires["base"] = map[string]any{"source": "./base"}
		requires["other"] = map[string]any{"source": "./other"}
	})
	dropPlatform(t, h)
	removeLock(t, h)

	code, stdout, _ := h.run(t, "lock", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "modpack-platform" {
		t.Fatalf("locked modpacks built for different Minecraft versions should be refused: code=%d %s", code, stdout)
	}
}

func TestNoMinecraftAndNoModpackToInheritFrom(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "create", "--loader", "fabric")
	h.editManifest(t, func(m map[string]any) { delete(m, "minecraft") })
	removeLock(t, h)

	code, stdout, _ := h.run(t, "lock", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "minecraft-required" {
		t.Fatalf("a project with no minecraft and nothing to inherit it from should be refused: code=%d %s", code, stdout)
	}
}
