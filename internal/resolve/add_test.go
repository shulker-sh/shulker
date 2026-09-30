package resolve

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/env/envtest"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

// twoHosts is a first provider with sodium and fabric-api, and a sha1-only second one with jei,
// sodium and fabric-api, the way Modrinth and CurseForge overlap.
func twoHosts(t *testing.T) (*envtest.Host, *envtest.Host, *harness) {
	t.Helper()
	c := envtest.NewCDN(t)
	alpha, beta := envtest.NewHost(c, "alpha"), envtest.NewHost(c, "beta")
	beta.LikeCurseForge()
	alpha.Publish(mod("a-fapi", "fabric-api"), provider.Version{Number: "0.130.0", File: provider.File{Filename: "fabric-api-0.130.0+26.2.jar"}}, modJar(t, "fabric-api", "1.0.0", "*"))
	alpha.Publish(mod("a-sodium", "sodium"), provider.Version{Number: "0.9.2", File: provider.File{Filename: "sodium-fabric-0.9.2+mc26.2.jar"}, Dependencies: []provider.Dependency{dependsOn("a-fapi")}}, modJar(t, "sodium", "1.0.0", "client"))
	beta.Publish(mod("306612", "fabric-api"), provider.Version{ID: "5000010", Number: "0.130.0", File: provider.File{Filename: "fabric-api-0.130.0+26.2.jar"}}, modJar(t, "fabric-api", "1.0.0", "*"))
	beta.Publish(mod("394468", "sodium"), provider.Version{ID: "5000020", Number: "0.9.2", File: provider.File{Filename: "sodium-fabric-0.9.2+mc26.2.jar"}, Dependencies: []provider.Dependency{dependsOn("306612")}}, modJar(t, "sodium", "1.0.0", "client"))
	beta.Publish(mod("238222", "jei"), provider.Version{ID: "5000001", Number: "1.0.0", File: provider.File{Filename: "jei-26.2-fabric-1.0.0.jar"}, Dependencies: []provider.Dependency{dependsOn("306612")}}, modJar(t, "jei", "1.0.0", "*"))
	return alpha, beta, newHarness(t, alpha, beta)
}

func TestAddFallsThroughToTheNextProvider(t *testing.T) {
	_, beta, h := twoHosts(t)
	h.mustAdd("jei", AddOptions{})

	jei := h.mod("jei")
	jar := h.cdn.Bytes(beta.Files[2])
	if jei.Provider != "beta" || jei.Project != "238222" || jei.Version != "5000001" || jei.Sha512 != sha512Hex(jar) || jei.Size != int64(len(jar)) || jei.URL == nil || jei.Page != "" || jei.Side != "both" {
		t.Fatalf("jei lock entry: %+v", jei)
	}
	if dep := h.mod("fabric-api"); dep.Provider != "beta" || dep.RequiredBy[0] != "jei" {
		t.Fatalf("fabric-api lock entry: %+v", dep)
	}
	if entry := h.r.Manifest.Mods()["jei"]; entry.Project != "238222" || entry.Provider != "beta" {
		t.Fatalf("jei manifest entry: %+v", entry)
	}
	if _, ok := h.r.Manifest.Requires["fabric-api"]; ok {
		t.Fatal("a dependency is not a manifest entry")
	}

	err := h.add("nothing-anywhere", AddOptions{})
	if e := out.AsError(err); e == nil || e.Code != "mod-not-found" || !strings.Contains(e.Message, "Alpha or Beta") {
		t.Fatalf("expected mod-not-found, got %v", err)
	}

	h.mustAdd("394468", AddOptions{})
	if sodium := h.mod("sodium"); sodium.Provider != "beta" || h.r.Manifest.Mods()["sodium"].Project != "394468" {
		t.Fatalf("add by id: %+v", sodium)
	}
}

func TestOutdatedUpdateAndPin(t *testing.T) {
	_, beta, h := twoHosts(t)
	h.mustAdd("jei", AddOptions{})
	ctx := context.Background()

	if got, err := h.r.Outdated(ctx, nil); err != nil || len(got) != 0 {
		t.Fatalf("outdated before a new file: %v %v", got, err)
	}
	beta.Publish(mod("238222", "jei"), provider.Version{ID: "5000002", Number: "1.1.0", File: provider.File{Filename: "jei-26.2-fabric-1.1.0.jar"}, Dependencies: []provider.Dependency{dependsOn("306612")}}, modJar(t, "jei", "1.1.0", "*"))
	got, err := h.r.Outdated(ctx, nil)
	if err != nil || len(got) != 1 || got[0] != (Outdated{ID: "jei", Current: "1.0.0", Latest: "1.1.0"}) {
		t.Fatalf("outdated: %+v %v", got, err)
	}
	if err := h.r.Update(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if v := h.mod("jei").Version; v != "5000002" {
		t.Fatalf("update left version %s", v)
	}
	if pinned, err := h.r.Pin(ctx, "jei", "5000001"); err != nil || pinned != "5000001" {
		t.Fatalf("pin: %s %v", pinned, err)
	}
	if pin := h.r.Manifest.Mods()["jei"].Pin; pin != "5000001" {
		t.Fatalf("pin: %v", pin)
	}
	if v := h.mod("jei").Version; v != "5000001" {
		t.Fatalf("pin left version %s", v)
	}
	if got, _ := h.r.Outdated(ctx, nil); len(got) != 1 || !got[0].Pinned {
		t.Fatalf("outdated after a pin: %+v", got)
	}
	if err := h.r.Unpin(ctx, "jei"); err != nil || h.mod("jei").Version != "5000002" {
		t.Fatalf("unpin: %v %+v", err, h.mod("jei"))
	}
}

func TestAddSwitchesProviderAndKeepsTheOtherAsAlias(t *testing.T) {
	_, _, h := twoHosts(t)
	h.mustAdd("sodium", AddOptions{})
	if sodium := h.mod("sodium"); sodium.Provider != "alpha" {
		t.Fatalf("first add: %+v", sodium)
	}

	before := h.r.Snapshot()
	h.mustAdd("sodium", AddOptions{Provider: "beta"})
	sodium := h.mod("sodium")
	if sodium.Provider != "beta" || sodium.Project != "394468" || sodium.Aliases["alpha"] != "a-sodium" || sodium.Aliases["beta"] != "" {
		t.Fatalf("switched entry: %+v", sodium)
	}
	if c := h.r.Changes(before); len(c.Added) != 0 || len(c.Updated) != 1 || c.Updated[0].FromProvider != "alpha" || c.Updated[0].ToProvider != "beta" {
		t.Fatalf("a switch is reported as an update between providers: %+v", c)
	}
	if !h.logged("switching sodium from alpha to beta") {
		t.Fatalf("log: %v", h.log)
	}
	if by := h.mod("fabric-api").RequiredBy; len(by) != 1 || by[0] != "sodium" {
		t.Fatalf("fabric-api requiredBy after switch: %v", by)
	}
	if entry := h.r.Manifest.Mods()["sodium"]; entry.Provider != "beta" || entry.Project != "394468" {
		t.Fatalf("manifest after switch: %+v", entry)
	}

	h.mustAdd("sodium", AddOptions{Provider: "alpha"})
	sodium = h.mod("sodium")
	if sodium.Provider != "alpha" || sodium.Aliases["beta"] != "394468" || sodium.Aliases["alpha"] != "" {
		t.Fatalf("switched back entry: %+v", sodium)
	}
	if entry := h.r.Manifest.Mods()["sodium"]; entry.Provider != "" || entry.Project != "" {
		t.Fatalf("manifest after switching back to the first provider by slug: %+v", entry)
	}

	h.mustAdd("sodium", AddOptions{Provider: "beta"})
	h.mustAdd("sodium", AddOptions{})
	if sodium := h.mod("sodium"); sodium.Provider != "beta" {
		t.Fatalf("a plain add should keep the switched provider: %+v", sodium)
	}
}

func TestAddNamesAProviderItCannotReach(t *testing.T) {
	_, beta, h := twoHosts(t)
	beta.Unavailable = &out.Error{Code: "provider-unavailable", Message: "beta needs an API key", Help: "set SHULKER_BETA_KEY"}

	err := h.add("jei", AddOptions{Provider: "beta"})
	if e := out.AsError(err); e == nil || e.Code != "provider-unavailable" || !strings.Contains(e.Help, "SHULKER_BETA_KEY") {
		t.Fatalf("expected provider-unavailable naming the key, got %v", err)
	}
	err = h.add("jei", AddOptions{})
	if e := out.AsError(err); e == nil || e.Code != "mod-not-found" || e.Message != "jei was not found on Alpha" || len(e.Items) != 1 || e.Items[0] != "skipped: beta needs an API key" {
		t.Fatalf("expected a miss naming the skipped provider, got %v", err)
	}
}

func TestAddTakesAManualDownloadFromTheDownloadsFolder(t *testing.T) {
	c := envtest.NewCDN(t)
	cf := envtest.NewHost(c, "curse").LikeCurseForge()
	nodist := modJar(t, "nodist", "1.0.0", "client")
	v := cf.PublishManual(mod("300000", "nodist"), provider.Version{ID: "5100001", Number: "1.0.0", File: provider.File{Filename: "nodist-1.0.0.jar"}}, nodist)
	h := newHarness(t, cf)

	err := h.add("nodist", AddOptions{})
	if e := out.AsError(err); e == nil || e.Code != "manual-download" || !slices.Contains(e.Items, v.Page) || !strings.Contains(e.Message, "nodist-1.0.0.jar") || !strings.Contains(e.Rows[0].Text, DownloadsDir+"/") {
		t.Fatalf("expected manual-download, got %v", err)
	}

	h.drop("nodist-1.0.0.jar", nodist)
	h.mustAdd("nodist", AddOptions{})
	got := h.mod("nodist")
	if got.URL != nil || got.Page != v.Page || got.Sha512 != sha512Hex(nodist) || got.Side != "client" {
		t.Fatalf("nodist lock entry: %+v", got)
	}
}

func TestAddTreatsAForbiddenDownloadAsManual(t *testing.T) {
	c := envtest.NewCDN(t)
	cf := envtest.NewHost(c, "curse").LikeCurseForge()
	locked := modJar(t, "locked", "1.0.0", "*")
	v := cf.Publish(mod("400000", "locked"), provider.Version{ID: "5200001", Number: "1.0.0", File: provider.File{Filename: "locked-1.0.0.jar"}}, locked)
	c.Forbid(v)
	h := newHarness(t, cf)

	err := h.add("locked", AddOptions{})
	if e := out.AsError(err); e == nil || e.Code != "manual-download" || !slices.Contains(e.Items, v.Page) {
		t.Fatalf("expected manual-download after 403, got %v", err)
	}
	if !h.logged("treating locked 1.0.0 as distribution-disabled: download forbidden") {
		t.Fatalf("log: %v", h.log)
	}
	h.drop("locked-1.0.0.jar", locked)
	h.mustAdd("locked", AddOptions{})
	if got := h.mod("locked"); got.URL != nil || got.Page != v.Page {
		t.Fatalf("locked lock entry: %+v", got)
	}
}

func TestAManualDownloadTheCacheHoldsIsTakenFromIt(t *testing.T) {
	c := envtest.NewCDN(t)
	cf := envtest.NewHost(c, "curse").LikeCurseForge()
	nodist, locked := modJar(t, "nodist", "1.0.0", "client"), modJar(t, "locked", "1.0.0", "*")
	cf.PublishManual(mod("300000", "nodist"), provider.Version{ID: "5100001", Number: "1.0.0", File: provider.File{Filename: "nodist-1.0.0.jar"}}, nodist)
	c.Forbid(cf.Publish(mod("400000", "locked"), provider.Version{ID: "5200001", Number: "1.0.0", File: provider.File{Filename: "locked-1.0.0.jar"}}, locked))
	h := newHarness(t, cf)
	h.drop("nodist-1.0.0.jar", nodist)
	h.drop("locked-1.0.0.jar", locked)
	h.mustAdd("nodist", AddOptions{})
	h.mustAdd("locked", AddOptions{})
	if !h.r.Cache.IsManual(h.mod("nodist").Sha512) || !h.r.Cache.IsManual(h.mod("locked").Sha512) {
		t.Fatal("a file taken from downloads/ is marked manual in the cache")
	}

	if err := os.RemoveAll(filepath.Join(h.r.Dir, DownloadsDir)); err != nil {
		t.Fatal(err)
	}
	h.nextCommand()
	for _, id := range []string{"nodist", "locked"} {
		delete(h.r.Lock.Mods, id)
		delete(h.r.Manifest.Requires, id)
		h.mustAdd(id, AddOptions{})
		if got := h.mod(id); got.URL != nil || got.Sha512 == "" {
			t.Fatalf("%s is locked as a manual download: %+v", id, got)
		}
	}
	if !h.logged("taking nodist-1.0.0.jar from the cache") || !h.logged("taking locked-1.0.0.jar from the cache") {
		t.Fatalf("each take from the cache is a step: %q", h.log)
	}
}

func TestInstallFillsAPendingDownloadFromTheCache(t *testing.T) {
	h := newHarness(t, envtest.NewHost(envtest.NewCDN(t), "alpha"))
	jar := modJar(t, "rtg", "1.0.0", "*")
	if _, err := h.r.Cache.PutManual(bytes.NewReader(jar)); err != nil {
		t.Fatal(err)
	}
	sum := sha1.Sum(jar)
	h.r.Lock.Mods["rtg"] = lock.Mod{Provider: "alpha", Project: "a-rtg", Version: "v1", VersionNumber: "1.0.0", Filename: "rtg.jar", Page: "https://example.com/rtg", Sha1: hex.EncodeToString(sum[:]), Side: "both", RequiredBy: []string{}}
	if _, _, err := h.install(); err != nil {
		t.Fatalf("a pending download the cache holds isn't missing: %v", err)
	}
	if m := h.mod("rtg"); m.IsPending() || m.Sha512 != sha512Hex(jar) {
		t.Fatalf("the cached copy fills the pending entry: %+v", m)
	}
	if !h.logged("took 1 manual download from the cache") {
		t.Fatalf("the take is reported: %q", h.log)
	}
}

func TestInstallWarnsOfStrayDownloadsAndListsMissingFiles(t *testing.T) {
	c := envtest.NewCDN(t)
	cf := envtest.NewHost(c, "curse").LikeCurseForge()
	nodist, locked := modJar(t, "nodist", "1.0.0", "client"), modJar(t, "locked", "1.0.0", "*")
	cf.PublishManual(mod("300000", "nodist"), provider.Version{ID: "5100001", Number: "1.0.0", File: provider.File{Filename: "nodist-1.0.0.jar"}}, nodist)
	forbidden := cf.Publish(mod("400000", "locked"), provider.Version{ID: "5200001", Number: "1.0.0", File: provider.File{Filename: "locked-1.0.0.jar"}}, locked)
	c.Forbid(forbidden)
	h := newHarness(t, cf)
	h.drop("nodist-1.0.0.jar", nodist)
	h.drop("locked-1.0.0.jar", locked)
	h.mustAdd("nodist", AddOptions{})
	h.mustAdd("locked", AddOptions{})

	h.drop("unrelated.jar", []byte("not a mod"))
	h.drop("log.json", nil)
	h.r.Cache = &cache.Cache{Dir: t.TempDir()}
	fetched, warnings, err := h.install()
	if err != nil || len(fetched) != 0 || len(warnings) != 1 || warnings[0] != DownloadsDir+"/unrelated.jar matches no mod in the lock." {
		t.Fatalf("install: %v %v %v", fetched, warnings, err)
	}
	for _, id := range []string{"nodist", "locked"} {
		if !h.r.Cache.Has(h.mod(id).Sha512) || !h.r.Cache.IsManual(h.mod(id).Sha512) {
			t.Fatalf("%s was not taken from %s/ as a manual download", id, DownloadsDir)
		}
	}
	if h.r.Cache.IsManual(sha512Hex([]byte("not a mod"))) {
		t.Fatal("a stray file in downloads/ is not a manual download, so a prune may drop it")
	}

	if err := os.RemoveAll(filepath.Join(h.r.Dir, DownloadsDir)); err != nil {
		t.Fatal(err)
	}
	h.r.Cache = &cache.Cache{Dir: t.TempDir()}
	_, _, err = h.install()
	e := out.AsError(err)
	if e == nil || e.Code != "missing-files" || len(e.Items) != 2 || !strings.Contains(e.Items[0], forbidden.Page) || !strings.Contains(e.Items[1], "nodist-1.0.0.jar") {
		t.Fatalf("expected missing-files, got %v", err)
	}
}

// betaDependency gives a host a library with only beta files, older one first, and a release mod
// that requires it, the shape Framework and Goblin Traders have.
func betaDependency(t *testing.T, cf *envtest.Host) {
	t.Helper()
	framework := mod("667391", "framework-fabric")
	cf.Publish(framework, provider.Version{ID: "5600002", Number: "0.6.17", Channel: "beta", Published: day(10), File: provider.File{Filename: "framework-fabric-0.6.17.jar"}}, modJar(t, "framework", "0.6.17", "*"))
	cf.Publish(framework, provider.Version{ID: "5600001", Number: "0.6.16", Channel: "beta", Published: day(1), File: provider.File{Filename: "framework-fabric-0.6.16.jar"}}, modJar(t, "framework", "0.6.16", "*"))
	cf.Publish(mod("667389", "goblin-traders-fabric"), provider.Version{ID: "5600011", Number: "1.9.3", File: provider.File{Filename: "goblintraders-fabric-1.9.3.jar"}, Dependencies: []provider.Dependency{dependsOn("667391")}}, modJar(t, "goblintraders", "1.9.3", "*"))
}

func TestPinningABetaFileAcceptsBeta(t *testing.T) {
	cf := envtest.NewHost(envtest.NewCDN(t), "curse").LikeCurseForge()
	betaDependency(t, cf)
	h := newHarness(t, cf)

	h.mustAdd("667391", AddOptions{Pin: "5600001"})

	if !slices.Contains(h.r.Warnings, "framework 0.6.16 is a beta; accepting beta for it.") {
		t.Fatalf("add should say the pin widens the channel: %v", h.r.Warnings)
	}
	if got := h.mod("framework").Channel; got != "beta" {
		t.Fatalf("lock channel = %q, want beta", got)
	}
	if entry := h.r.Manifest.Mods()["framework"]; entry.Channel != "beta" || entry.Pin != "5600001" || entry.Project != "667391" {
		t.Fatalf("manifest entry: %+v", entry)
	}
	if err := h.add("667391", AddOptions{Pin: "5600011"}); out.CodeOf(err) != "pin-mismatch" {
		t.Fatalf("a pin from another project: %v", err)
	}
	if err := h.add("667391", AddOptions{Pin: "nope"}); out.CodeOf(err) != "version-not-found" || !strings.Contains(out.AsError(err).Help, cf.VersionsPage(manifest.TypeMod, "framework-fabric")) {
		t.Fatalf("a pin that is no version: %v", err)
	}
}

func TestPinWidensTheChannel(t *testing.T) {
	_, beta, h := twoHosts(t)
	beta.Publish(mod("238222", "jei"), provider.Version{ID: "5000003", Number: "1.0.1-beta", Channel: "beta", File: provider.File{Filename: "jei-26.2-fabric-1.0.1-beta.jar"}}, modJar(t, "jei", "1.0.1-beta", "*"))
	h.mustAdd("jei", AddOptions{})
	if v := h.mod("jei").Version; v != "5000001" {
		t.Fatalf("a plain add should skip the beta: %s", v)
	}

	if _, err := h.r.Pin(context.Background(), "jei", "5000003"); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(h.r.Warnings, "jei 1.0.1-beta is a beta; accepting beta for it.") {
		t.Fatalf("pin should say the pin widens the channel: %v", h.r.Warnings)
	}
	if got := h.r.Manifest.Mods()["jei"].Channel; got != "beta" {
		t.Fatalf("manifest channel = %q, want beta", got)
	}
	if got := h.mod("jei"); got.Channel != "beta" || got.Version != "5000003" {
		t.Fatalf("lock entry: %+v", got)
	}
}

func TestADependencyAlreadyLockedKeepsItsVersion(t *testing.T) {
	cf := envtest.NewHost(envtest.NewCDN(t), "curse").LikeCurseForge()
	betaDependency(t, cf)
	h := newHarness(t, cf)
	h.mustAdd("667391", AddOptions{Pin: "5600001"})

	h.mustAdd("667389", AddOptions{})

	framework := h.mod("framework")
	if framework.Version != "5600001" || !slices.Contains(framework.RequiredBy, "goblintraders") {
		t.Fatalf("framework should stay at its pinned file and gain goblintraders: %+v", framework)
	}
	if _, ok := h.r.Lock.Mods["framework-fabric"]; ok {
		t.Fatalf("the dependency was locked a second time: %+v", h.r.Lock.Mods)
	}
}

// pinnedSodium is a host with sodium 0.9.2 and 0.9.3, and iris depending on sodium 0.9.2's file.
func pinnedSodium(t *testing.T) *harness {
	t.Helper()
	host := envtest.NewHost(envtest.NewCDN(t), "alpha")
	host.Publish(mod("a-sodium", "sodium"), provider.Version{ID: "s-092", Number: "0.9.2", Published: day(1), File: provider.File{Filename: "sodium-0.9.2.jar"}}, modJar(t, "sodium", "0.9.2", "client"))
	host.Publish(mod("a-sodium", "sodium"), provider.Version{ID: "s-093", Number: "0.9.3", Published: day(2), File: provider.File{Filename: "sodium-0.9.3.jar"}}, modJar(t, "sodium", "0.9.3", "client"))
	host.Publish(mod("a-iris", "iris"), provider.Version{ID: "i-100", Number: "1.0.0", Published: day(1), File: provider.File{Filename: "iris-1.0.0.jar"}, Dependencies: []provider.Dependency{{VersionID: "s-092", Type: "required"}}}, modJar(t, "iris", "1.0.0", "client"))
	return newHarness(t, host)
}

func TestOneCommandLocksTheVersionADependentAsksFor(t *testing.T) {
	for _, order := range [][]string{{"sodium", "iris"}, {"iris", "sodium"}} {
		h := pinnedSodium(t)

		for _, slug := range order {
			h.mustAdd(slug, AddOptions{})
		}

		if got := h.mod("sodium"); got.VersionNumber != "0.9.2" || !slices.Contains(got.RequiredBy, "iris") {
			t.Fatalf("add %v: iris asks for sodium 0.9.2, so the command locks it: %+v", order, got)
		}
		if len(h.notes) > 0 || len(h.r.Warnings) > 0 {
			t.Fatalf("add %v: settling on the version iris asks for says nothing: %+v %q", order, h.notes, h.r.Warnings)
		}
	}
}

func TestAnExactVersionAskedOfAHeldModWarns(t *testing.T) {
	h := pinnedSodium(t)
	h.mustAdd("sodium", AddOptions{})

	h.nextCommand()
	h.mustAdd("iris", AddOptions{})

	if got := h.mod("sodium").VersionNumber; got != "0.9.3" {
		t.Fatalf("sodium was in the pack before, so it stays: %s", got)
	}
	want := "iris 1.0.0 asks for sodium 0.9.2, but the pack keeps 0.9.3; `shulker pin sodium s-092` locks it, or add with --with-deps."
	if len(h.r.Warnings) != 1 || h.r.Warnings[0] != want || len(h.notes) > 0 {
		t.Fatalf("warnings %q, notes %+v", h.r.Warnings, h.notes)
	}
}

func TestWithDepsMovesAHeldModToTheExactVersionAskedFor(t *testing.T) {
	h := pinnedSodium(t)
	h.mustAdd("sodium", AddOptions{})

	h.nextCommand()
	h.mustAdd("iris", AddOptions{WithDeps: true})

	if got := h.mod("sodium"); got.VersionNumber != "0.9.2" || !slices.Contains(got.RequiredBy, "iris") {
		t.Fatalf("--with-deps moves sodium to the version iris asks for: %+v", got)
	}
	if len(h.r.Warnings) > 0 {
		t.Fatalf("warnings %q", h.r.Warnings)
	}
}

func TestAPinnedModKeepsItsVersionOverAnExactAsk(t *testing.T) {
	h := pinnedSodium(t)

	h.mustAdd("sodium", AddOptions{Pin: "s-093"})
	h.mustAdd("iris", AddOptions{WithDeps: true})

	if got := h.mod("sodium").VersionNumber; got != "0.9.3" {
		t.Fatalf("a pin is the user's own choice: %s", got)
	}
	if len(h.r.Warnings) != 1 || !strings.Contains(h.r.Warnings[0], "iris 1.0.0 asks for sodium 0.9.2, but sodium is pinned to 0.9.3") {
		t.Fatalf("warnings %q", h.r.Warnings)
	}
}

func TestANamedModIsNotShownAsRequired(t *testing.T) {
	_, _, h := twoHosts(t)
	before := h.r.Snapshot()

	h.mustAdd("fabric-api", AddOptions{})
	h.mustAdd("sodium", AddOptions{})

	c := h.r.Changes(before)
	i := slices.IndexFunc(c.Added, func(m AddedMod) bool { return m.ID == "fabric-api" })
	if i < 0 || len(c.Added[i].RequiredBy) != 0 {
		t.Fatalf("fabric-api was named, so its change shows no required-by: %+v", c.Added)
	}
	if got := h.mod("fabric-api").RequiredBy; !slices.Equal(got, []string{"sodium"}) {
		t.Fatalf("the lock still records sodium requiring fabric-api: %q", got)
	}
	if err := h.r.Remove([]string{"sodium"}); err != nil {
		t.Fatal(err)
	}
	if got := h.mod("fabric-api").RequiredBy; len(got) != 0 {
		t.Fatalf("removing sodium leaves fabric-api named and required by nothing: %q", got)
	}
}

func TestAddRefusesAVersionOffTheChannel(t *testing.T) {
	cf := envtest.NewHost(envtest.NewCDN(t), "curse").LikeCurseForge()
	betaDependency(t, cf)
	h := newHarness(t, cf)

	err := h.add("667391", AddOptions{})
	e := out.AsError(err)
	if e == nil || e.Code != "no-compatible-version" || e.Message != "framework-fabric has no release version for Minecraft 26.2 with fabric" || e.Flag != "--channel" || !slices.Equal(e.Candidates, []string{"0.6.17 (beta)", "0.6.16 (beta)"}) {
		t.Fatalf("expected no-compatible-version listing the betas, got %+v", e)
	}
	err = h.add("667389", AddOptions{})
	if e := out.AsError(err); e == nil || e.Code != "no-compatible-version" || !strings.HasPrefix(e.Message, "dependency of goblintraders: ") {
		t.Fatalf("a dependency off the channel: %v", err)
	}
	h.mustAdd("667391", AddOptions{Channel: "beta"})
	if got := h.mod("framework"); got.Version != "5600002" || got.Channel != "beta" || h.r.Manifest.Mods()["framework"].Channel != "beta" {
		t.Fatalf("beta add: %+v", got)
	}
}

func TestPinningABetaPackFileAcceptsBeta(t *testing.T) {
	cf := curseForgeHost(t)
	fresh := cf.Known[3]
	cf.Publish(fresh, provider.Version{ID: "5300002", Number: "1.9.5", Channel: "beta", Published: day(10), Loaders: []string{}, File: provider.File{Filename: "FreshAnimations_CF_v1.9.5.zip"}}, cf.CDN.Bytes(cf.Files[3]))
	h := newHarness(t, cf)

	h.mustAdd("600000", AddOptions{Pin: "5300002"})

	if !slices.ContainsFunc(h.r.Warnings, func(w string) bool { return strings.Contains(w, "is a beta; accepting beta for it") }) {
		t.Fatalf("the pin should widen the channel: %v", h.r.Warnings)
	}
	if got := h.r.Lock.ResourcePacks["fresh-animations"]; got.Channel != "beta" || got.Version != "5300002" {
		t.Fatalf("lock entry: %+v", got)
	}
	if got := h.r.Manifest.Requires["fresh-animations"]; got.Channel != "beta" || got.Type != manifest.TypeResourcePack {
		t.Fatalf("manifest entry: %+v", got)
	}
}

func TestASlugTheProviderLacksCarriesItsHelp(t *testing.T) {
	cf := envtest.NewHost(envtest.NewCDN(t), "curseforge").LikeCurseForge()
	cf.Help = "add one its search misses by its project id"
	cf.Publish(mod("500525", "balm-fabric"), provider.Version{ID: "5700001", Number: "7.3.9", File: provider.File{Filename: "balm-fabric-7.3.9.jar"}}, modJar(t, "balm", "7.3.9", "*"))
	h := newHarness(t, cf)

	for _, opts := range []AddOptions{{Provider: "curseforge"}, {}} {
		err := h.add("balm-forge", opts)
		if e := out.AsError(err); e == nil || e.Code != "mod-not-found" || !strings.Contains(e.Help, "by its project id") {
			t.Fatalf("%+v: %v", opts, err)
		}
	}
	h.mustAdd("500525", AddOptions{Provider: "curseforge"})
	if got := h.mod("balm"); got.Project != "500525" {
		t.Fatalf("an add by project id: %+v", got)
	}
}

func TestAddLocksABlockedFileFromAnotherProviderHostingItsBytes(t *testing.T) {
	c := envtest.NewCDN(t)
	alpha := envtest.NewHost(c, "alpha")
	cf := envtest.NewHost(c, "curse").LikeCurseForge()
	iris := modJar(t, "iris", "1.0.0", "client")
	mirrored := alpha.Publish(mod("YL57", "irisshaders"), provider.Version{ID: "v1", Number: "1.0.0", File: provider.File{Filename: "iris-1.0.0.jar"}}, iris)
	cf.PublishManual(mod("300002", "iris-cf"), provider.Version{ID: "5100002", Number: "1.0.0", File: provider.File{Filename: "iris-1.0.0.jar"}}, iris)
	h := newHarness(t, alpha, cf)

	h.mustAdd("iris-cf", AddOptions{Provider: "curse"})
	got := h.mod("iris")
	if got.Provider != "alpha" || got.Project != "YL57" || got.URL == nil || *got.URL != mirrored.File.URL || got.Sha512 != sha512Hex(iris) {
		t.Fatalf("iris lock entry: %+v", got)
	}
	if entry := h.r.Manifest.Requires["iris"]; entry.Provider != "" || entry.Project != "YL57" {
		t.Fatalf("iris manifest entry: %+v", entry)
	}
	if !slices.Contains(h.r.Warnings, "iris-cf: taken from Alpha (Curse blocks third-party downloads)") {
		t.Fatalf("add should say where the file was locked from: %v", h.r.Warnings)
	}
	if alpha.Requests["IdentifySHA1"] != 1 || cf.Requests["IdentifySHA1"] != 0 {
		t.Fatalf("the blocking provider isn't asked for its own file: %v %v", alpha.Requests, cf.Requests)
	}
}

func TestANamedVersionIsLockedAsTheJarsOwn(t *testing.T) {
	c := envtest.NewCDN(t)
	alpha := envtest.NewHost(c, "alpha").LikeCurseForge()
	alpha.Publish(mod("a-apple", "appleskin"), provider.Version{Number: "appleskin-neoforge-mc1.21-3.0.9.jar", File: provider.File{Filename: "appleskin.jar"}}, modJar(t, "appleskin", "3.0.9", "*"))
	h := newHarness(t, alpha)
	h.mustAdd("a-apple", AddOptions{})
	if got := h.mod("appleskin").VersionNumber; got != "3.0.9" {
		t.Fatalf("version %q, want the jar's 3.0.9", got)
	}
}

func TestASha1OnlyFileTheCacheHoldsIsNotDownloadedAgain(t *testing.T) {
	c := envtest.NewCDN(t)
	alpha := envtest.NewHost(c, "alpha").LikeCurseForge()
	v := alpha.Publish(mod("a-apple", "appleskin"), provider.Version{Number: "3.0.9", File: provider.File{Filename: "appleskin.jar"}}, modJar(t, "appleskin", "3.0.9", "*"))
	h := newHarness(t, alpha)
	h.mustAdd("a-apple", AddOptions{})
	c.Truncate(v)
	h.nextCommand()
	delete(h.r.Lock.Mods, "appleskin")
	delete(h.r.Manifest.Requires, "appleskin")
	if err := h.add("a-apple", AddOptions{}); err != nil {
		t.Fatalf("the cached copy should be taken rather than downloaded again: %v", err)
	}
}

func TestInstallAdoptsAPendingDownloadBySha1(t *testing.T) {
	c := envtest.NewCDN(t)
	alpha := envtest.NewHost(c, "alpha")
	jar := modJar(t, "rtg", "1.0.0", "*")
	h := newHarness(t, alpha)
	sum := sha1.Sum(jar)
	h.r.Lock.Mods["rtg"] = lock.Mod{Provider: "alpha", Project: "a-rtg", Version: "v1", VersionNumber: "1.0.0", Filename: "rtg.jar", Page: "https://example.com/rtg", Sha1: hex.EncodeToString(sum[:]), Side: "both", RequiredBy: []string{}}
	if !h.mod("rtg").IsPending() {
		t.Fatal("a mod locked without its bytes is pending")
	}
	if _, _, err := h.install(); out.CodeOf(err) != "missing-files" {
		t.Fatalf("a pending mod is a manual download: %v", err)
	}
	h.drop("RTG-1.0.0-renamed.jar", jar)
	if _, _, err := h.install(); err != nil {
		t.Fatal(err)
	}
	m := h.mod("rtg")
	if m.IsPending() || m.Sha512 != sha512Hex(jar) || m.Size != int64(len(jar)) || !h.r.Cache.Has(m.Sha512) {
		t.Fatalf("the dropped file fills the pending entry: %+v", m)
	}
	if h.r.Adopted() == nil {
		t.Fatal("the resolver reports that it changed the lock")
	}
}

func TestAnAddThatMissesBeforeChangingAnythingIsSkippable(t *testing.T) {
	host := envtest.NewHost(envtest.NewCDN(t), "alpha")
	host.Publish(mod("a-old", "old"), provider.Version{Number: "1.0", GameVersions: []string{"26.3"}, File: provider.File{Filename: "old-1.0.jar"}}, modJar(t, "old", "1.0", "*"))
	host.Publish(mod("a-parent", "parent"), provider.Version{Number: "1.0", File: provider.File{Filename: "parent-1.0.jar"}, Dependencies: []provider.Dependency{dependsOn("a-old")}}, modJar(t, "parent", "1.0", "*"))
	h := newHarness(t, host)

	for _, slug := range []string{"nope", "old"} {
		before := h.r.Snapshot()
		if err := h.add(slug, AddOptions{}); !h.r.Missed(before, err) {
			t.Errorf("%s: %v should be skippable", slug, err)
		}
	}
	before := h.r.Snapshot()
	if err := h.add("parent", AddOptions{}); err == nil || h.r.Missed(before, err) {
		t.Errorf("parent's dependency failed after parent was locked, which isn't skippable: %v", err)
	}
}
