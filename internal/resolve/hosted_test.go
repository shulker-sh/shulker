package resolve

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

// hostedCraftPack publishes the craft pack on cf as the modpack craftpack, with one beta file.
func hostedCraftPack(t *testing.T, cf *host) provider.Version {
	t.Helper()
	path := filepath.Join(t.TempDir(), "craft-1.1.zip")
	writeCurseForgeZip(t, path, craftFiles, map[string]string{})
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	craftpack := provider.Project{ID: "800000", Slug: "craftpack", Title: "Craft Pack", Type: manifest.TypeModpack}
	return cf.publish(craftpack, provider.Version{ID: "7000002", Number: "1.1", Channel: "beta", Published: day(10), Loaders: []string{"fabric"}, File: provider.File{Filename: "craft-1.1.zip"}}, data)
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
	if pin.Channel != "beta" || pin.Version != "7000002" || pin.Provider != "curseforge" || pin.Sha512 != sha512Hex(cf.cdn.bytes(beta)) || !h.r.Cache.Has(pin.Sha512) {
		t.Fatalf("modpack pin: %+v", pin)
	}
	if got := h.r.Manifest.Requires["craftpack"].Channel; got != "beta" {
		t.Fatalf("manifest channel = %q, want beta", got)
	}
}

func TestAModpackMemberPinnedToABetaFileLocksBeta(t *testing.T) {
	cf := newHost(newCDN(t), "curseforge").likeCurseForge()
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
	release := cf.publish(craftpack, provider.Version{ID: "7000001", Number: "1.0", Loaders: []string{"fabric"}, File: provider.File{Filename: "craft-1.0.zip"}}, data)
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
	manual := cf.publishManual(craftpack, provider.Version{ID: "7000001", Number: "1.0", Loaders: []string{"fabric"}, File: provider.File{Filename: "craft-1.0.zip"}}, data)
	h := newHarness(t, cf)
	entry := manifest.Require{Type: manifest.TypeModpack}

	_, err = h.r.ObtainModpack(context.Background(), "craftpack", entry)
	if e := out.AsError(err); e == nil || e.Code != "manual-download" || !strings.Contains(e.Help, manual.Page) || !strings.Contains(e.Help, "craft-1.0.zip") {
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
