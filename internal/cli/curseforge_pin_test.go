package cli

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
)

// betaDependency gives CurseForge a library with only beta files, older one first, and a
// release mod that requires it, the shape Framework and Goblin Traders have.
func betaDependency(t *testing.T, h *harness) {
	t.Helper()
	h.cfMods[667391] = &cfMod{id: 667391, slug: "framework-fabric", files: []cfFile{
		{id: 5600002, jar: makeJarVersion(t, "framework", "framework-fabric-0.6.17.jar", "*", "0.6.17", `"depends":{"fabricloader":">=0.17"}`), date: "2026-09-10T00:00:00Z", channel: 2},
		{id: 5600001, jar: makeJarVersion(t, "framework", "framework-fabric-0.6.16.jar", "*", "0.6.16", `"depends":{"fabricloader":">=0.17"}`), date: "2026-09-01T00:00:00Z", channel: 2},
	}}
	h.cfMods[667389] = &cfMod{id: 667389, slug: "goblin-traders-fabric", files: []cfFile{
		{id: 5600011, jar: makeJar(t, "goblintraders", "goblintraders-fabric-1.9.3.jar", "*"), date: "2026-09-01T00:00:00Z", channel: 1, deps: []int{667391}},
	}}
}

func TestPinningABetaFileAcceptsBeta(t *testing.T) {
	h := newHarness(t)
	betaDependency(t, h)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")

	_, _, stderr := h.run(t, "add", "667391", "--provider", "curseforge", "--pin", "5600001")

	if !strings.Contains(stderr, "framework framework-fabric-0.6.16 is a beta; accepting beta for it") {
		t.Fatalf("add should say the pin widens the channel: %s", stderr)
	}
	if got := h.readLock(t).Mods["framework"].Channel; got != "beta" {
		t.Fatalf("lock channel = %q, want beta", got)
	}
	if got := h.readManifest(t).Mods()["framework"].Channel; got != "beta" {
		t.Fatalf("manifest channel = %q, want beta", got)
	}
	var env out.Envelope
	if err := json.Unmarshal([]byte(h.mustRun(t, "install", "--json")), &env); err != nil {
		t.Fatal(err)
	}
	if env.LockStale {
		t.Fatalf("the lock should match the manifest: %+v", env)
	}
}

func TestPinCommandWidensTheChannel(t *testing.T) {
	h := newHarness(t)
	jei := h.cfMods[238222]
	jei.files = append(jei.files, cfFile{id: 5000003, jar: makeJarVersion(t, "jei", "jei-26.2-fabric-1.0.1-beta.jar", "*", "1.0.1-beta", `"depends":{"fabricloader":">=0.17"}`), date: "2026-08-15T00:00:00Z", channel: 2, deps: []int{306612}})
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	h.mustRun(t, "add", "jei")

	_, _, stderr := h.run(t, "pin", "jei", "5000003")

	if !strings.Contains(stderr, "jei jei-26.2-fabric-1.0.1-beta is a beta; accepting beta for it") {
		t.Fatalf("pin should say the pin widens the channel: %s", stderr)
	}
	if got := h.readManifest(t).Mods()["jei"].Channel; got != "beta" {
		t.Fatalf("manifest channel = %q, want beta", got)
	}
	if got := h.readLock(t).Mods["jei"].Channel; got != "beta" {
		t.Fatalf("lock channel = %q, want beta", got)
	}
}

func TestADependencyAlreadyLockedKeepsItsVersion(t *testing.T) {
	h := newHarness(t)
	betaDependency(t, h)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	h.mustRun(t, "add", "667391", "--provider", "curseforge", "--pin", "5600001")

	h.mustRun(t, "add", "667389", "--provider", "curseforge")

	l := h.readLock(t)
	framework := l.Mods["framework"]
	if framework.Version != "5600001" || !strings.Contains(strings.Join(framework.RequiredBy, ","), "goblintraders") {
		t.Fatalf("framework should stay at its pinned file and gain goblintraders: %+v", framework)
	}
	if _, ok := l.Mods["framework-fabric"]; ok {
		t.Fatalf("the dependency was locked a second time: %+v", l.Mods)
	}
}

func TestACurseForgeSlugItsSearchMissesPointsAtTheProjectID(t *testing.T) {
	h := newHarness(t)
	h.cfMods[500525] = &cfMod{id: 500525, slug: "balm-fabric", unlisted: true, files: []cfFile{
		{id: 5700001, jar: makeJar(t, "balm", "balm-fabric-7.3.9.jar", "*"), date: "2026-09-01T00:00:00Z", channel: 1},
	}}
	h.mustRun(t, "init", "--yes", "--loader", "fabric")

	for _, args := range [][]string{{"add", "balm-fabric", "--provider", "curseforge", "--json"}, {"add", "balm-fabric", "--json"}} {
		code, stdout, _ := h.run(t, args...)
		var env out.Envelope
		_ = json.Unmarshal([]byte(stdout), &env)
		if code == 0 || env.Error == nil || env.Error.Code != "mod-not-found" || !strings.Contains(env.Error.Help, "by its project id") {
			t.Fatalf("%v: code=%d error=%+v", args, code, env.Error)
		}
	}
	h.mustRun(t, "add", "500525", "--provider", "curseforge")
}

func TestAModpackMemberPinnedToABetaFileLeavesTheLockCurrent(t *testing.T) {
	h := newHarness(t)
	betaDependency(t, h)
	h.mustRun(t, "init", "--yes", "--loader", "fabric")
	writePrismPack(t, filepath.Join(h.dir, "base"), "~26.2", `"framework": {"provider": "curseforge", "project": "667391", "pin": "5600001"}`, nil)

	h.mustRun(t, "modpack", "add", "./base", "--unlocked")

	if got := h.readLock(t).Mods["framework"].Channel; got != "beta" {
		t.Fatalf("lock channel = %q, want beta", got)
	}
	if env := installJSON(t, h); env.LockStale {
		t.Fatalf("the lock should match the manifest: %+v", env)
	}
}

func TestPinningABetaPackFileAcceptsBeta(t *testing.T) {
	h := newHarness(t)
	fresh := h.cfMods[600000]
	fresh.files = append(fresh.files, cfFile{id: 5300002, jar: h.jars["cf-fresh-animations"], date: "2026-09-10T00:00:00Z", channel: 2})
	h.mustRun(t, "init", "--yes", "--loader", "fabric")

	_, _, stderr := h.run(t, "add", "600000", "--provider", "curseforge", "--pin", "5300002")

	if !strings.Contains(stderr, "is a beta; accepting beta for it") {
		t.Fatalf("add should say the pin widens the channel: %s", stderr)
	}
	if got := h.readLock(t).Packs("resourcepack")["fresh-animations"].Channel; got != "beta" {
		t.Fatalf("lock channel = %q, want beta", got)
	}
	if got := h.readManifest(t).Requires["fresh-animations"].Channel; got != "beta" {
		t.Fatalf("manifest channel = %q, want beta", got)
	}
	if env := installJSON(t, h); env.LockStale {
		t.Fatalf("the lock should match the manifest: %+v", env)
	}
}

func TestPinningABetaModpackFileAcceptsBeta(t *testing.T) {
	h := archiveProject(t)
	path := filepath.Join(t.TempDir(), "craft-1.1.zip")
	writeCurseForgeZip(t, path, importedCurseForgePack(craftFiles...), map[string][]byte{})
	archive := archiveJar(t, h, "craft-1.1.zip", path)
	h.cfMods[800000] = &cfMod{id: 800000, slug: "craftpack", class: 4471, files: []cfFile{{id: 7000002, jar: archive, date: "2026-09-10T00:00:00Z", channel: 2}}}

	_, _, stderr := h.run(t, "add", "craftpack", "--pin", "7000002")

	if !strings.Contains(stderr, "is a beta; accepting beta for it") {
		t.Fatalf("add should say the pin widens the channel: %s", stderr)
	}
	if got := h.readLock(t).Modpacks["craftpack"].Channel; got != "beta" {
		t.Fatalf("lock channel = %q, want beta", got)
	}
	if got := h.readManifest(t).Requires["craftpack"].Channel; got != "beta" {
		t.Fatalf("manifest channel = %q, want beta", got)
	}
	if env := installJSON(t, h); env.LockStale {
		t.Fatalf("the lock should match the manifest: %+v", env)
	}
}
