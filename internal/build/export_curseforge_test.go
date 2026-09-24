package build

import (
	"bytes"
	"maps"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

// curseForgeExport is a project with sodium and fabric-api locked from Modrinth and jei from
// CurseForge, where CurseForge also has sodium and fabric-api as the same bytes.
type curseForgeExport struct {
	*exportProject
	sodium, fabricAPI, jei provider.Version
}

func newCurseForgeExport(t *testing.T) *curseForgeExport {
	t.Helper()
	x := &curseForgeExport{exportProject: newExportProject(t)}
	sodium, api, jei := modJar(t, "sodium", "1.0.0"), modJar(t, "fabric-api", "1.0.0"), modJar(t, "jei", "1.0.0")
	x.lockMod("sodium", x.modrinth, x.modrinth.publish(mod("AANobbMI", "sodium"), provider.Version{ID: "m-sodium-1", Number: "0.9.2", File: provider.File{Filename: "sodium-fabric-0.9.2+mc26.2.jar"}}, sodium))
	x.lockMod("fabric-api", x.modrinth, x.modrinth.publish(mod("P7dR8mSH", "fabric-api"), provider.Version{ID: "m-api-1", Number: "0.130.0", File: provider.File{Filename: "fabric-api-0.130.0+26.2.jar"}}, api))
	x.fabricAPI = x.cf.publish(provider.Project{ID: "306612", Slug: "fabric-api", Title: "Fabric API", Author: "modmuss50"}, provider.Version{ID: "5000010", Number: "0.130.0", File: provider.File{Filename: "fabric-api-0.130.0+26.2.jar"}}, api)
	x.jei = x.cf.publish(provider.Project{ID: "238222", Slug: "jei", Title: "JEI", Author: "jei-dev"}, provider.Version{ID: "5000001", Number: "1.0.0", File: provider.File{Filename: "jei-26.2-fabric-1.0.0.jar"}}, jei)
	x.sodium = x.cf.publish(provider.Project{ID: "394468", Slug: "sodium", Title: "Sodium", Author: "jellysquid3"}, provider.Version{ID: "5000020", Number: "0.9.2", File: provider.File{Filename: "sodium-fabric-0.9.2+mc26.2.jar"}}, sodium)
	x.lockMod("jei", x.cf, x.jei)
	x.override("options.txt", "lang:en_us\n")
	return x
}

func failure(t *testing.T, err error) *out.Error {
	t.Helper()
	if err == nil {
		t.Fatal("the export should fail")
	}
	e := out.AsError(err)
	if e == nil {
		t.Fatalf("not a shulker error: %v", err)
	}
	return e
}

func TestExportListsModsFoundOnCurseForgeByID(t *testing.T) {
	x := newCurseForgeExport(t)
	report, err := x.exportCurseForge(false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Name != "Demo Pack" || report.Version != "1.0" || len(report.Mods) != 3 || strings.Join(report.Matched, ",") != "fabric-api,sodium" || len(report.BundledMods) != 0 {
		t.Fatalf("report: %+v", report)
	}
	if ids := listedIDs(x.listed()); strings.Join(ids, " ") != "306612/5000010 238222/5000001 394468/5000020" {
		t.Fatalf("listed: %v", ids)
	}
	entries := x.archive()
	if !strings.Contains(entries["modlist.html"], `<li><a href="https://curseforge.test/mod/jei">JEI (by jei-dev)</a></li>`) {
		t.Fatalf("modlist: %s", entries["modlist.html"])
	}
	if !strings.HasPrefix(entries["profileImage/icon.png"], "\x89PNG") || !strings.Contains(entries["shulker.json"], `"sodium"`) || !strings.Contains(entries["shulker.lock"], `"sodium"`) || entries["overrides/options.txt"] != "lang:en_us\n" {
		t.Fatalf("archive: %v", slices.Sorted(maps.Keys(entries)))
	}
	for name := range entries {
		if strings.HasPrefix(name, "overrides/mods/") && name != "overrides/mods/shulker-pack.jar" {
			t.Fatalf("a matched mod was bundled: %s", name)
		}
	}
}

func TestExportRefusesAModCurseForgeLacksUnlessBundled(t *testing.T) {
	x := newCurseForgeExport(t)
	x.cf.Files = slices.DeleteFunc(x.cf.Files, func(v provider.Version) bool { return v.ProjectID == "394468" })
	x.cf.Known = slices.DeleteFunc(x.cf.Known, func(p provider.Project) bool { return p.ID == "394468" })

	e := failure(t, mustFail(x.exportCurseForge(false)))
	if e.Code != "curseforge-not-found" || len(e.Items) != 1 || e.Items[0] != "sodium (modrinth, "+strings.TrimPrefix(x.cdn.srv.URL, "http://")+")" || e.Nudge.Command != "shulker export curseforge --bundle" {
		t.Fatalf("unmatched mod: %+v", e)
	}

	report, err := x.exportCurseForge(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Mods) != 2 || strings.Join(report.BundledMods, ",") != "sodium" || strings.Join(report.Matched, ",") != "fabric-api" || !slices.ContainsFunc(report.Warnings, func(w string) bool { return strings.HasPrefix(w, "bundled sodium from modrinth") }) {
		t.Fatalf("bundle report: %+v", report)
	}
	if entries := x.archive(); entries["overrides/mods/sodium-fabric-0.9.2+mc26.2.jar"] != string(modJar(t, "sodium", "1.0.0")) {
		t.Fatalf("bundled entries: %v", slices.Sorted(maps.Keys(entries)))
	}
}

func mustFail(_ *ExportReport, err error) error { return err }

func TestExportMatchesARezippedUploadByContents(t *testing.T) {
	x := newCurseForgeExport(t)
	locked := modJar(t, "sodium", "1.0.0")
	rezipped := rezip(t, locked, func(_, content string) string { return content })
	if bytes.Equal(rezipped, locked) {
		t.Fatal("the rezipped jar should differ from the Modrinth one")
	}
	x.cf.republish("5000020", rezipped)

	report, err := x.exportCurseForge(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Mods) != 3 || strings.Join(report.Matched, ",") != "fabric-api,sodium" {
		t.Fatalf("report: %+v", report)
	}
	if !slices.ContainsFunc(report.Warnings, func(w string) bool {
		return strings.Contains(w, "sodium matches CurseForge file 5000020 by contents, but its bytes differ from the locked file")
	}) {
		t.Fatalf("warnings: %v", report.Warnings)
	}
	if !slices.Contains(listedIDs(x.listed()), "394468/5000020") {
		t.Fatalf("listed: %v", listedIDs(x.listed()))
	}
	if x.cf.Requests["Identify"] != 1 || x.cf.Requests["Versions"] != 1 || !x.logged("comparing sodium with CurseForge file 5000020") {
		t.Fatalf("one fingerprint request, then one lookalike: %v %v", x.cf.Requests, x.log)
	}
}

func TestExportRejectsALookalikeWithOtherContents(t *testing.T) {
	x := newCurseForgeExport(t)
	locked := modJar(t, "sodium", "1.0.0")
	other := rezip(t, locked, func(_, content string) string { return strings.Replace(content, `"1.0.0"`, `"1.0.1"`, 1) })
	if len(other) != len(locked) {
		t.Fatal("the lookalike should keep the locked file's size")
	}
	x.cf.republish("5000020", other)

	e := failure(t, mustFail(x.exportCurseForge(false)))
	if e.Code != "curseforge-not-found" || len(e.Items) != 1 || !strings.HasPrefix(e.Items[0], "sodium (modrinth") {
		t.Fatalf("lookalike: %+v", e)
	}
}

func TestExportLeavesALookalikeItCannotDownload(t *testing.T) {
	x := newCurseForgeExport(t)
	v := x.cf.republish("5000020", rezip(t, modJar(t, "sodium", "1.0.0"), func(_, content string) string { return content }))
	x.cdn.forbid(v)

	e := failure(t, mustFail(x.exportCurseForge(false)))
	if e.Code != "curseforge-not-found" || len(e.Items) != 1 || !strings.HasPrefix(e.Items[0], "sodium (modrinth") {
		t.Fatalf("undownloadable lookalike: %+v", e)
	}
}

func TestExportBundlesEverythingWhenCurseForgeCannotBeAsked(t *testing.T) {
	x := newCurseForgeExport(t)
	x.cf.Unavailable = out.Errorf("provider-unavailable", "curseforge needs an API key")

	e := failure(t, mustFail(x.exportCurseForge(false)))
	if e.Code != "provider-unavailable" || strings.Join(e.Items, ",") != "fabric-api,sodium" {
		t.Fatalf("no key: %+v", e)
	}

	report, err := x.exportCurseForge(true)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(report.BundledMods, ",") != "fabric-api,sodium" || strings.Join(report.Mods, ",") != "jei" {
		t.Fatalf("bundle report: %+v", report)
	}
	for _, want := range []string{"CurseForge lookup failed, so these are bundled: curseforge needs an API key", "bundled fabric-api", "bundled sodium"} {
		if !slices.ContainsFunc(report.Warnings, func(w string) bool { return strings.Contains(w, want) }) {
			t.Fatalf("warnings lack %q: %v", want, report.Warnings)
		}
	}
}

func TestExportOfModsLockedFromCurseForgeNeedsNoLookup(t *testing.T) {
	x := newCurseForgeExport(t)
	delete(x.b.Lock.Mods, "sodium")
	delete(x.b.Manifest.Requires, "sodium")
	x.lockMod("fabric-api", x.cf, x.fabricAPI)
	x.cf.Unavailable = out.Errorf("provider-unavailable", "curseforge needs an API key")

	report, err := x.exportCurseForge(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Mods) != 2 || len(report.Matched) != 0 || x.cf.Requests["Identify"] != 0 || x.logged("looking up") {
		t.Fatalf("export of CurseForge-locked mods: %+v %v", report, x.log)
	}
	if !slices.ContainsFunc(report.Warnings, func(w string) bool { return strings.Contains(w, "the pack's listing names project IDs") }) {
		t.Fatalf("offline listing warning: %v", report.Warnings)
	}
	if modlist := x.archive()["modlist.html"]; !strings.Contains(modlist, `<li><a href="https://curseforge.test/project/238222">jei</a></li>`) {
		t.Fatalf("offline modlist: %s", modlist)
	}
}

func TestExportListsPacksLockedFromCurseForgeByID(t *testing.T) {
	x := newExportProject(t)
	x.lockMod("jei", x.cf, x.cf.publish(mod("238222", "jei"), provider.Version{ID: "5000001", Number: "1.0.0", File: provider.File{Filename: "jei-26.2-fabric-1.0.0.jar"}}, modJar(t, "jei", "1.0.0")))
	fresh := provider.Project{ID: "600000", Slug: "fresh-animations", Title: "Fresh Animations", Type: "resourcepack"}
	x.lockPack("resourcepack", "fresh-animations", x.cf, x.cf.publish(fresh, provider.Version{ID: "5300001", Number: "1.9.4", Loaders: []string{}, File: provider.File{Filename: "FreshAnimations_CF_v1.9.4.zip"}}, packZip(t, "fresh")))

	report, err := x.exportCurseForge(false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(report.Mods, ",") != "jei" || strings.Join(report.ResourcePacks, ",") != "fresh-animations" || len(report.BundledResourcePacks) != 0 {
		t.Fatalf("report: %+v", report)
	}
	if ids := listedIDs(x.listed()); strings.Join(ids, " ") != "238222/5000001 600000/5300001" {
		t.Fatalf("listed: %v", ids)
	}
	for name := range x.archive() {
		if strings.HasPrefix(name, "overrides/resourcepacks/") {
			t.Fatalf("a pack locked from CurseForge was bundled: %s", name)
		}
	}
}

func TestExportEnablesPacksByTheirCurseForgeNames(t *testing.T) {
	x := newExportProject(t)
	fresh := provider.Project{ID: "600000", Slug: "fresh-animations", Title: "Fresh Animations", Type: "resourcepack"}
	x.lockPack("resourcepack", "fresh-animations", x.cf, x.cf.publish(fresh, provider.Version{ID: "5300001", Number: "1.9.4", Loaders: []string{}, File: provider.File{Filename: "FreshAnimations_CF_v1.9.4.zip"}}, packZip(t, "fresh")))
	shader := packZip(t, "complementary")
	complementary := provider.Project{ID: "kPHBmpnE", Slug: "complementary-reimagined", Type: "shader"}
	x.lockPack("shader", "complementary-reimagined", x.modrinth, x.modrinth.publish(complementary, provider.Version{ID: "m-cr-1", Number: "r5.5.1", Loaders: []string{"iris"}, File: provider.File{Filename: "ComplementaryReimagined_r5.5.1.zip"}}, shader), "iris")
	x.cf.publish(provider.Project{ID: "455508", Slug: "complementary-reimagined", Type: "shader"}, provider.Version{ID: "5400001", Number: "r5.5.1", Loaders: []string{"iris"}, File: provider.File{Filename: "ComplementaryReimagined_r5.5.1.zip"}}, shader)
	x.lockMod("irisshaders", x.cf, x.cf.publish(mod("455508", "irisshaders"), provider.Version{ID: "5000030", Number: "1.8.0", File: provider.File{Filename: "iris-fabric-1.8.0+mc26.2.jar"}}, modJar(t, "iris", "1.8.0")))
	iris := x.b.Lock.Mods["irisshaders"]
	iris.ModID = "iris"
	x.b.Lock.Mods["irisshaders"] = iris

	if _, err := x.exportCurseForge(false); err != nil {
		t.Fatal(err)
	}
	entries := x.archive()
	if options := entries["overrides/options.txt"]; !strings.Contains(options, `"file/FreshAnimations_CF_v1.9.4.zip"`) || strings.Contains(options, "fresh-animations.zip") {
		t.Fatalf("options.txt: %s", options)
	}
	if props := entries["overrides/config/iris.properties"]; !strings.Contains(props, "shaderPack=ComplementaryReimagined_r5.5.1.zip") {
		t.Fatalf("iris.properties: %s", props)
	}
}
