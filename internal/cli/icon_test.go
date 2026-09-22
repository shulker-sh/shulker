package cli

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/launcher"
)

func squarePNG(t *testing.T, size int, c color.Color) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestExportCarriesThePackIcon(t *testing.T) {
	h := newCurseForgeExport(t)
	icon := squarePNG(t, 64, color.NRGBA{R: 200, A: 255})
	writeOverride(t, h.dir, "assets/pack.png", string(icon))
	h.editManifest(t, func(m map[string]any) { m["icon"] = "assets/pack.png" })

	h.mustRun(t, "export", "curseforge")
	entries := readArchive(t, filepath.Join(h.dir, "build", "pack-1.0.zip"))
	var pack curseForgePack
	if err := json.Unmarshal([]byte(entries["manifest.json"]), &pack); err != nil {
		t.Fatal(err)
	}
	if pack.Image != "profileImage/pack.png" || entries["profileImage/pack.png"] != string(icon) {
		t.Fatalf("profile image %q, entries %v", pack.Image, keys(entries))
	}
	if _, ok := entries["profileImage/icon.png"]; ok {
		t.Fatalf("the shulker icon stays out once the pack names its own: %v", keys(entries))
	}

	h.allowMrpackHost(t)
	h.mustRun(t, "export", "mrpack")
	_, mr := readMrpack(t, filepath.Join(h.dir, "build", "pack-1.0.mrpack"))
	if mr["icon.png"] != string(icon) {
		t.Fatalf("mrpack icon.png: %d bytes, entries %v", len(mr["icon.png"]), keys(mr))
	}
}

func TestExportIconFallsBackToTheMarker(t *testing.T) {
	h := newCurseForgeExport(t)
	h.allowMrpackHost(t)
	h.mustRun(t, "export", "mrpack")
	_, mr := readMrpack(t, filepath.Join(h.dir, "build", "pack-1.0.mrpack"))
	if !strings.HasPrefix(mr["icon.png"], "\x89PNG") {
		t.Fatalf("a marker pack carries the shulker icon: %v", keys(mr))
	}

	h.editManifest(t, func(m map[string]any) { m["marker"] = false })
	h.mustRun(t, "install")
	h.mustRun(t, "export", "mrpack")
	_, mr = readMrpack(t, filepath.Join(h.dir, "build", "pack-1.0.mrpack"))
	if _, ok := mr["icon.png"]; ok {
		t.Fatalf("a pack without the marker or an icon carries none: %v", keys(mr))
	}
	h.mustRun(t, "export", "curseforge")
	entries := readArchive(t, filepath.Join(h.dir, "build", "pack-1.0.zip"))
	if strings.Contains(entries["manifest.json"], `"image"`) {
		t.Fatalf("manifest.json: %s", entries["manifest.json"])
	}
}

func TestExportRefusesAnIconThatIsNotAPNG(t *testing.T) {
	h := newCurseForgeExport(t)
	writeOverride(t, h.dir, "assets/pack.png", "not a png")
	h.editManifest(t, func(m map[string]any) { m["icon"] = "assets/pack.png" })
	code, stdout, _ := h.run(t, "export", "curseforge", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "manifest-invalid" || !strings.Contains(e.Message, "assets/pack.png") {
		t.Fatalf("a non-PNG icon: exit %d %s", code, stdout)
	}

	h.editManifest(t, func(m map[string]any) { m["icon"] = "assets/missing.png" })
	h.allowMrpackHost(t)
	code, stdout, _ = h.run(t, "export", "mrpack", "--json")
	if e := failureCode(t, stdout); code == 0 || e.Code != "manifest-invalid" || !strings.Contains(e.Message, "assets/missing.png") {
		t.Fatalf("a missing icon: exit %d %s", code, stdout)
	}
}

func TestLinkedInstanceFollowsThePackIcon(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "my-pack")
	launcherDir := t.TempDir()
	h.mustRun(t, "link", "atlauncher", "--launcher-dir", launcherDir, "--name", "Friends SMP")
	instDir := filepath.Join(launcherDir, "instances", "FriendsSMP")
	card := filepath.Join(instDir, launcher.ATLauncherImageFile)
	if data, _ := os.ReadFile(card); !bytes.Equal(data, launcher.ATLauncherImage) {
		t.Fatal("a pack without an icon keeps the shulker card")
	}

	writeOverride(t, h.dir, "icon.png", string(squarePNG(t, 32, color.NRGBA{B: 255, A: 255})))
	h.editManifest(t, func(m map[string]any) { m["icon"] = "icon.png" })
	h.mustRun(t, "sync", "-i", "Friends SMP")
	data, err := os.ReadFile(card)
	if err != nil {
		t.Fatal(err)
	}
	img, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 300 || b.Dy() != 150 {
		t.Fatalf("card is %v", b)
	}
	if _, _, _, a := img.At(10, 75).RGBA(); a != 0 {
		t.Fatal("the card's sides are transparent")
	}
	if r, g, b, a := img.At(150, 75).RGBA(); a == 0 || b>>8 < 250 || r != 0 || g != 0 {
		t.Fatalf("the icon sits in the middle: %v %v %v %v", r, g, b, a)
	}

	picked := squarePNG(t, 16, color.NRGBA{G: 255, A: 255})
	if err := os.WriteFile(card, picked, 0o644); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "sync", "-i", "Friends SMP")
	if data, _ := os.ReadFile(card); !bytes.Equal(data, picked) {
		t.Fatal("an image the player picked survives while the pack's icon is unchanged")
	}

	writeOverride(t, h.dir, "icon.png", string(squarePNG(t, 32, color.NRGBA{R: 255, A: 255})))
	h.mustRun(t, "sync", "-i", "Friends SMP")
	if data, _ := os.ReadFile(card); bytes.Equal(data, picked) {
		t.Fatal("a changed pack icon replaces the card")
	}
}

func TestLinkedGDLauncherInstanceTakesThePackIcon(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "my-pack")
	icon := squarePNG(t, 48, color.NRGBA{R: 9, G: 9, B: 9, A: 255})
	writeOverride(t, h.dir, "icon.png", string(icon))
	h.editManifest(t, func(m map[string]any) { m["icon"] = "icon.png" })
	launcherDir := t.TempDir()
	h.mustRun(t, "link", "gdlauncher", "--launcher-dir", launcherDir, "--name", "Friends SMP")
	matches, _ := filepath.Glob(filepath.Join(launcherDir, "instances", "*", launcher.GDLauncherIconFile))
	if len(matches) != 1 {
		t.Fatalf("icons: %v", matches)
	}
	if data, _ := os.ReadFile(matches[0]); !bytes.Equal(data, icon) {
		t.Fatal("GDLauncher's icon is the pack icon as it is")
	}
}

func TestImportMrpackRestoresTheIcon(t *testing.T) {
	h := newHarness(t)
	h.allowMrpackHost(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	icon := squarePNG(t, 24, color.NRGBA{R: 1, G: 2, B: 3, A: 255})
	writeOverride(t, h.dir, "assets/pack.png", string(icon))
	h.editManifest(t, func(m map[string]any) { m["icon"] = "assets/pack.png" })
	h.mustRun(t, "export", "mrpack", "--version", "1.0.0")

	dir := filepath.Join(t.TempDir(), "imported")
	h.mustRun(t, "import", "mrpack", filepath.Join(h.dir, "build", "pack-1.0.0.mrpack"), "--dir", dir)
	if m, _ := readProject(t, dir); m.Icon != "assets/pack.png" {
		t.Fatalf("icon key: %q", m.Icon)
	}
	if data, _ := os.ReadFile(filepath.Join(dir, "assets", "pack.png")); !bytes.Equal(data, icon) {
		t.Fatal("the archive's icon.png goes back where the manifest names it")
	}
}
