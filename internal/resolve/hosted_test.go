package resolve

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/env/envtest"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

// hostedCraftPack publishes the craft pack on cf as the modpack craftpack, with one beta file.
func hostedCraftPack(t *testing.T, cf *envtest.Host) provider.Version {
	t.Helper()
	path := filepath.Join(t.TempDir(), "craft-1.1.zip")
	writeCurseForgeZip(t, path, craftFiles, map[string]string{})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	craftpack := provider.Project{ID: "800000", Slug: "craftpack", Title: "Craft Pack", Type: manifest.TypeModpack}
	return cf.Publish(craftpack, provider.Version{ID: "7000002", Number: "1.1", Channel: "beta", Published: day(10), Loaders: []string{"fabric"}, File: provider.File{Filename: "craft-1.1.zip"}}, data)
}

func TestPinningABetaModpackFileAcceptsBeta(t *testing.T) {
	cf := curseForgeHost(t)
	beta := hostedCraftPack(t, cf)
	h := newHarness(t, cf)
	entry := manifest.Require{Type: manifest.TypeModpack, Provider: "curseforge", Project: "800000", Pin: "7000002"}
	h.r.Manifest.Requires["craftpack"] = entry

	pin, err := h.r.ObtainModpack(context.Background(), "craftpack", entry)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(h.r.Warnings, func(w string) bool { return strings.Contains(w, "is a beta; accepting beta for it") }) {
		t.Fatalf("the pin should widen the channel: %v", h.r.Warnings)
	}
	if pin.Channel != "beta" || pin.Version != "7000002" || pin.Provider != "curseforge" || pin.Sha512 != sha512Hex(cf.CDN.Bytes(beta)) || !h.r.Cache.Has(pin.Sha512) {
		t.Fatalf("modpack pin: %+v", pin)
	}
	if got := h.r.Manifest.Requires["craftpack"].Channel; got != "beta" {
		t.Fatalf("manifest channel = %q, want beta", got)
	}
}

func TestAModpackMemberPinnedToABetaFileLocksBeta(t *testing.T) {
	cf := envtest.NewHost(envtest.NewCDN(t), "curseforge").LikeCurseForge()
	betaDependency(t, cf)
	h := newHarness(t, cf)
	base := &modpack.Loaded{Name: "base", Kind: modpack.Local, Manifest: &manifest.Manifest{
		Name: "base", Minecraft: "~26.2", Loader: manifest.Loader{Type: "fabric"},
		Requires: map[string]manifest.Require{"framework": {Provider: "curseforge", Project: "667391", Pin: "5600001"}},
	}}

	if err := h.r.AddPack(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	if got := h.mod("framework"); got.Channel != "beta" || got.Version != "5600001" || got.Modpack != "" || !slices.Contains(got.RequiredBy, "base") {
		t.Fatalf("a pack member pinned to a beta: %+v", got)
	}
	if _, listed := h.r.Manifest.Requires["framework"]; listed {
		t.Fatalf("a pack's member is not the project's own: %+v", h.r.Manifest.Requires)
	}
}

func TestObtainModpackLocksAHostedCurseForgePack(t *testing.T) {
	cf := curseForgeHost(t)
	path := filepath.Join(t.TempDir(), "craft-1.0.zip")
	writeCurseForgeZip(t, path, craftFiles, map[string]string{"extras/config/jei.toml": "jei = hosted\n"})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	craftpack := provider.Project{ID: "800000", Slug: "craftpack", Title: "Craft Pack", Type: manifest.TypeModpack}
	release := cf.Publish(craftpack, provider.Version{ID: "7000001", Number: "1.0", Loaders: []string{"fabric"}, File: provider.File{Filename: "craft-1.0.zip"}}, data)
	h := newHarness(t, cf)

	pin, err := h.r.ObtainModpack(context.Background(), "craftpack", manifest.Require{Type: manifest.TypeModpack})
	if err != nil {
		t.Fatal(err)
	}
	if pin.Provider != "curseforge" || pin.Project != "800000" || pin.Version != "7000001" || pin.VersionNumber != "1.0" || pin.Sha512 != sha512Hex(data) || pin.URL == nil || *pin.URL != release.File.URL || pin.Filename != "craft-1.0.zip" || pin.Size != int64(len(data)) || !h.r.Cache.Has(pin.Sha512) {
		t.Fatalf("modpack pin: %+v", pin)
	}
}

func TestObtainModpackTakesAnUndistributedPackFromTheDownloadsFolder(t *testing.T) {
	cf := curseForgeHost(t)
	path := filepath.Join(t.TempDir(), "craft-1.0.zip")
	writeCurseForgeZip(t, path, craftFiles, map[string]string{"extras/config/jei.toml": "jei = manual\n"})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	craftpack := provider.Project{ID: "800000", Slug: "craftpack", Title: "Craft Pack", Type: manifest.TypeModpack}
	manual := cf.PublishManual(craftpack, provider.Version{ID: "7000001", Number: "1.0", Loaders: []string{"fabric"}, File: provider.File{Filename: "craft-1.0.zip"}}, data)
	h := newHarness(t, cf)
	entry := manifest.Require{Type: manifest.TypeModpack}

	_, err = h.r.ObtainModpack(context.Background(), "craftpack", entry)
	if e := out.AsError(err); e == nil || e.Code != "manual-download" || !slices.Contains(e.Items, manual.Page) || !strings.Contains(e.Message, "craft-1.0.zip") {
		t.Fatalf("an undistributed pack asks for a manual download: %v", err)
	}

	h.drop("craft-1.0.zip", data)
	pin, err := h.r.ObtainModpack(context.Background(), "craftpack", entry)
	if err != nil {
		t.Fatal(err)
	}
	if pin.URL != nil || pin.Page != manual.Page || pin.Sha512 != sha512Hex(data) || !h.r.Cache.Has(pin.Sha512) {
		t.Fatalf("modpack pin: %+v", pin)
	}
}

func TestAModpackFileTaggedWithNoLoaderFitsAnyLoader(t *testing.T) {
	cf := curseForgeHost(t)
	craftpack := provider.Project{ID: "800000", Slug: "craftpack", Title: "Craft Pack", Type: manifest.TypeModpack}
	cf.Publish(craftpack, provider.Version{ID: "7000001", Number: "1.0", Published: day(1), Loaders: []string{"fabric"}, File: provider.File{Filename: "craft-1.0.zip"}}, []byte("1.0"))
	cf.Publish(craftpack, provider.Version{ID: "7000002", Number: "1.1", Published: day(2), Loaders: []string{}, File: provider.File{Filename: "craft-1.1.zip"}}, []byte("1.1"))
	cf.Publish(craftpack, provider.Version{ID: "7000003", Number: "1.2", Published: day(3), Loaders: []string{"neoforge"}, File: provider.File{Filename: "craft-1.2.zip"}}, []byte("1.2"))
	cf.Publish(craftpack, provider.Version{ID: "7000004", Number: "2.0", Published: day(4), GameVersions: []string{"26.3"}, File: provider.File{Filename: "craft-2.0.zip"}}, []byte("2.0"))
	h := newHarness(t, cf)

	pin, err := h.r.ObtainModpack(context.Background(), "craftpack", manifest.Require{Type: manifest.TypeModpack})
	if err != nil {
		t.Fatal(err)
	}

	if pin.VersionNumber != "1.1" {
		t.Fatalf("the newest fabric or untagged release for Minecraft 26.2 is 1.1, got %s", pin.VersionNumber)
	}
}

func TestAReimportNeverGoesBackPastTheVersionLastImported(t *testing.T) {
	cf := curseForgeHost(t)
	craftpack := provider.Project{ID: "800000", Slug: "craftpack", Title: "Craft Pack", Type: manifest.TypeModpack}
	cf.Publish(craftpack, provider.Version{ID: "7000001", Number: "1.0", Published: day(1), Loaders: []string{"fabric"}, File: provider.File{Filename: "craft-1.0.zip"}}, []byte("1.0"))
	cf.Publish(craftpack, provider.Version{ID: "7000002", Number: "1.1-beta", Channel: "beta", Published: day(2), Loaders: []string{"fabric"}, File: provider.File{Filename: "craft-1.1.zip"}}, []byte("1.1"))
	h := newHarness(t, cf)
	was := &lock.Imported{Provider: "curseforge", Project: "800000", Version: "7000002", Sha512: sha512Hex([]byte("1.1"))}

	pin, err := h.r.ObtainImport(context.Background(), provider.Ref{Project: "craftpack"}, "", was)
	if err != nil {
		t.Fatal(err)
	}

	if pin.VersionNumber != "1.1-beta" {
		t.Fatalf("the project last imported 1.1-beta, so a re-import keeps it over the older release, got %s", pin.VersionNumber)
	}
}

func TestAnImportOfAFileURLTakesThatFile(t *testing.T) {
	cf := curseForgeHost(t)
	craftpack := provider.Project{ID: "800000", Slug: "craftpack", Title: "Craft Pack", Type: manifest.TypeModpack}
	cf.Publish(craftpack, provider.Version{ID: "7000001", Number: "1.0", Published: day(1), Loaders: []string{"fabric"}, File: provider.File{Filename: "craft-1.0.zip"}}, []byte("1.0"))
	cf.Publish(craftpack, provider.Version{ID: "7000002", Number: "1.1", Published: day(2), Loaders: []string{"fabric"}, File: provider.File{Filename: "craft-1.1.zip"}}, []byte("1.1"))
	h := newHarness(t, cf)
	was := &lock.Imported{Provider: "curseforge", Project: "800000", Version: "7000002", Sha512: sha512Hex([]byte("1.1"))}

	pin, err := h.r.ObtainImport(context.Background(), provider.Ref{Provider: "curseforge", Project: "craftpack", Version: "7000001"}, "", was)
	if err != nil {
		t.Fatal(err)
	}

	if pin.Provider != "curseforge" || pin.Project != "800000" || pin.VersionNumber != "1.0" {
		t.Fatalf("the URL names CurseForge's craftpack file 1.0, got %s %s %s", pin.Provider, pin.Project, pin.VersionNumber)
	}
}

func TestAnImportURLDisagreeingWithProviderIsRefused(t *testing.T) {
	cf := curseForgeHost(t)
	h := newHarness(t, cf)

	_, err := h.r.ObtainImport(context.Background(), provider.Ref{Provider: "curseforge", Project: "craftpack", Version: "7000001"}, "modrinth", nil)

	if out.CodeOf(err) != "usage" {
		t.Fatalf("--provider modrinth disagrees with a CurseForge URL, got %v", err)
	}
}
