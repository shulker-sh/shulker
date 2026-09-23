package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func runError(t *testing.T, h *harness, args ...string) *out.Error {
	t.Helper()
	code, stdout, _ := h.run(t, append(args, "--json")...)
	var env out.Envelope
	_ = json.Unmarshal([]byte(stdout), &env)
	if code == 0 || env.Error == nil {
		t.Fatalf("%v should fail: code=%d %s", args, code, stdout)
	}
	return env.Error
}

func TestAddPinsAModrinthVersionURLByItsNumber(t *testing.T) {
	h := newHarness(t)
	h.newer = true
	h.mustRun(t, "init", "--yes", "--loader", "fabric")

	h.mustRun(t, "add", "https://modrinth.com/mod/sodium/version/1.0.0+mc26.2")

	if got := h.readLock(t).Mods["sodium"].Version; got != "QANobbMI" {
		t.Fatalf("sodium locked at %v, want the 1.0.0 version QANobbMI", got)
	}
	if got := h.readManifest(t).Mods()["sodium"].Pin; got != "QANobbMI" {
		t.Fatalf("manifest pin = %v, want QANobbMI", got)
	}
}

func TestAddTakesSeveralURLsEachWithItsOwnPin(t *testing.T) {
	h := newHarness(t)
	h.newer = true
	h.cfMods[500525] = &cfMod{id: 500525, slug: "balm-fabric", unlisted: true, files: []cfFile{
		{id: 5700001, jar: makeJar(t, "balm", "balm-fabric-7.3.9.jar", "*"), date: "2026-09-01T00:00:00Z", channel: 1},
	}}
	h.mustRun(t, "init", "--yes", "--loader", "fabric")

	h.mustRun(t, "add",
		"https://www.curseforge.com/minecraft/mc-mods/balm-fabric/files/5700001",
		"https://legacy.curseforge.com/minecraft/mc-mods/jei/download/5000001",
		"https://cdn.modrinth.com/data/AANobbMI/versions/QANobbMI/sodium.jar",
		"https://modrinth.com/resourcepack/fresh-animations",
	)

	l := h.readLock(t)
	for key, want := range map[string]string{"balm": "5700001", "jei": "5000001", "sodium": "QANobbMI"} {
		if got := fmt.Sprint(l.Mods[key].Version); got != want {
			t.Fatalf("%s locked at %s, want %s", key, got, want)
		}
		if got := fmt.Sprint(h.readManifest(t).Mods()[key].Pin); got != want {
			t.Fatalf("%s pinned to %s, want %s", key, got, want)
		}
	}
	if l.Mods["balm"].Provider != "curseforge" {
		t.Fatalf("balm should come from curseforge: %+v", l.Mods["balm"])
	}
	if _, ok := l.ResourcePacks["fresh-animations"]; !ok {
		t.Fatalf("the resource pack URL should add a resource pack: %+v", l.ResourcePacks)
	}
	if pin := h.readManifest(t).Requires["fresh-animations"].Pin; pin != nil {
		t.Fatalf("a project URL is no pin: %v", pin)
	}
}

func TestAddRefusesFlagsThatDisagreeWithTheURL(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")

	for _, args := range [][]string{
		{"add", "https://modrinth.com/mod/sodium", "--provider", "curseforge"},
		{"add", "https://www.curseforge.com/minecraft/mc-mods/jei/files/5000001", "--pin", "5000000"},
		{"add", "https://modrinth.com/mod/sodium/version/QANobbMI", "https://modrinth.com/mod/fabric-api", "--pin", "QANobbMI"},
		{"add", "https://modrinth.com/mod/sodium/versions"},
	} {
		if e := runError(t, h, args...); e.Code != "usage" {
			t.Fatalf("%v: %+v, want a usage error", args, e)
		}
	}
	h.mustRun(t, "add", "https://modrinth.com/mod/sodium/version/1.0.0+mc26.2", "--provider", "modrinth", "--pin", "QANobbMI")
}

func TestAHostedModpackURL(t *testing.T) {
	h := newInPlace(t)
	archive := hostedMrpack(t, h, "cozy-1.0.0.mrpack", "1.0.0")
	h.modrinthPacks = map[string]*modrinthPack{"COZYpack": {slug: "cozy", versions: []modrinthPackVersion{{id: "cozyV100", number: "1.0.0", published: "2026-09-01T00:00:00Z", archive: archive}}}}

	h.mustRun(t, "add", "--type", "modpack", "https://modrinth.com/modpack/cozy/version/1.0.0")

	if got := h.readLock(t).Modpacks["cozy"].Version; got != "cozyV100" {
		t.Fatalf("cozy locked at %v, want cozyV100", got)
	}
}

func TestPinTakesAURLOfTheLockedProject(t *testing.T) {
	h := newHarness(t)
	h.newer = true
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	h.mustRun(t, "add", "jei")

	if e := runError(t, h, "pin", "nope", "https://modrinth.com/mod/sodium/version/QANobbMI"); e.Code != "mod-not-found" {
		t.Fatalf("pinning a key the manifest lacks: %+v", e)
	}
	for _, url := range []string{
		"https://modrinth.com/mod/sodium/version/QANobbMI",
		"https://www.curseforge.com/minecraft/mc-mods/sodium/files/5000020",
		"https://www.curseforge.com/minecraft/mc-mods/jei",
	} {
		if e := runError(t, h, "pin", "jei", url); e.Code != "usage" {
			t.Fatalf("%s: %+v, want a usage error", url, e)
		}
	}

	h.mustRun(t, "pin", "jei", "https://www.curseforge.com/minecraft/mc-mods/jei/files/5000001")

	if got := fmt.Sprint(h.readLock(t).Mods["jei"].Version); got != "5000001" {
		t.Fatalf("jei locked at %s, want 5000001", got)
	}
}

func TestAnUnlistedCurseForgeProjectURLAsksForAFileURL(t *testing.T) {
	h := newHarness(t)
	h.cfMods[500525] = &cfMod{id: 500525, slug: "balm-fabric", unlisted: true}
	h.mustRun(t, "init", "--yes", "--loader", "fabric")

	e := runError(t, h, "add", "https://www.curseforge.com/minecraft/mc-mods/balm-fabric")

	if e.Code != "mod-not-found" || !strings.Contains(e.Help, "file URL") {
		t.Fatalf("%+v", e)
	}
}
