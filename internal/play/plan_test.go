package play

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/env/envtest"
	"shulker.sh/shulker/internal/game"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

func TestAssembleFillsTheStoreForAVanillaInstance(t *testing.T) {
	h := newHarness(t, "")

	plan := h.mustAssemble(Request{})

	l := plan.Launchable
	if l.ID != "26.2" || l.Top.InheritsFrom != "" || l.Version.MainClass != "net.minecraft.client.main.Main" || l.Version.AssetIndex.ID != "26" {
		t.Fatalf("launchable %+v", l)
	}
	if len(l.Assembly.Libraries)+1 != 2 || l.Assembly.ClasspathSize(h.e.Store) == 0 {
		t.Fatalf("classpath %+v", l.Assembly)
	}
	icon := envtest.Sha1Hex([]byte(h.env.Piston.Assets["icons/icon_16x16.png"]))
	for _, rel := range []string{
		"versions/26.2/26.2.json",
		"versions/26.2/26.2.jar",
		"libraries/com/mojang/brigadier/1.3.10/brigadier-1.3.10.jar",
		"assets/indexes/26.json",
		"assets/objects/" + icon[:2] + "/" + icon,
		"launcher_profiles.json",
	} {
		if !exists(filepath.Join(h.e.Store.Root, filepath.FromSlash(rel))) {
			t.Fatalf("the store is missing %s", rel)
		}
	}
	if plan.Natives != instance.NativesDir(h.in.Dir) || plan.Instance.Dir != h.in.Dir {
		t.Fatalf("directories %+v", plan)
	}
	if plan.Java == "" || !exists(plan.Java) {
		t.Fatalf("the managed runtime should be in place: %q", plan.Java)
	}
	if plan.Sync != nil {
		t.Fatalf("a request that asks for no sync gets none: %+v", plan.Sync)
	}
	if exists(filepath.Join(h.e.Store.Root, "loaders.json")) {
		t.Fatal("a vanilla instance runs no loader installer")
	}

	before := h.env.Piston.Hits.Load()
	h.mustAssemble(Request{})
	if h.env.Piston.Hits.Load() != before {
		t.Fatalf("a second assembly downloaded %d files", h.env.Piston.Hits.Load()-before)
	}
}

func TestAssembleMergesTheLoaderOverVanilla(t *testing.T) {
	h := newHarness(t, "fabric")

	plan := h.mustAssemble(Request{})

	l := plan.Launchable
	if l.ID != "fabric-loader-0.17.3-26.2" || l.Top.InheritsFrom != "26.2" {
		t.Fatalf("launchable %+v", l)
	}
	if l.Version.MainClass != "net.fabricmc.loader.impl.launch.knot.KnotClient" {
		t.Fatalf("the loader's main class should win: %+v", l.Version)
	}
	// The loader's own jar, vanilla's library, and the client jar.
	if len(l.Assembly.Libraries)+1 != 3 {
		t.Fatalf("classpath %+v", l.Assembly)
	}
	if !exists(filepath.Join(h.e.Store.Root, "libraries", "net", "fabricmc", "fabric-loader", "0.17.3", "fabric-loader-0.17.3.jar")) {
		t.Fatal("the loader's library is missing from the store")
	}
	if !exists(filepath.Join(h.e.Store.Root, "versions", "26.2", "26.2.json")) {
		t.Fatal("the version it inherits from is missing from the store")
	}
}

func TestAssembleSyncsFirstUnlessAskedNotTo(t *testing.T) {
	h := newHarness(t, "")

	synced := h.mustAssemble(Request{Sync: true, Reason: "play"})
	if synced.Sync == nil || synced.Sync.Build == nil {
		t.Fatalf("a launch syncs before it assembles: %+v", synced.Sync)
	}
	if skipped := h.mustAssemble(Request{}); skipped.Sync != nil {
		t.Fatalf("--no-sync syncs nothing: %+v", skipped.Sync)
	}

	h.editInstance(func(f *instance.File) { f.Settings.Hooks.PreLaunch = instance.Off() })
	if off := h.mustAssemble(Request{Sync: true, Reason: "play"}); off.Sync != nil {
		t.Fatalf("hooks.preLaunch off syncs nothing: %+v", off.Sync)
	}
}

func TestAssembleTakesMemoryFromTheInstanceThenConfigThenPackThenDefault(t *testing.T) {
	h := newHarness(t, "")

	if plan := h.mustAssemble(Request{}); plan.Settings.Memory != instance.DefaultMemory {
		t.Fatalf("with nothing set the launch gets the fixed default, not the JVM's own: %+v", plan.Settings)
	}
	h.editManifest(func(m *manifest.Manifest) { m.Client.Memory = "6G" })
	if plan := h.mustAssemble(Request{}); plan.Settings.Memory != "6G" {
		t.Fatalf("the pack's client.memory beats the default: %+v", plan.Settings)
	}
	h.setConfig("play", "memory", "8G")
	if plan := h.mustAssemble(Request{}); plan.Settings.Memory != "8G" {
		t.Fatalf("play.memory beats the pack's client.memory: %+v", plan.Settings)
	}
	h.editInstance(func(f *instance.File) { f.Settings.Memory = "10G" })
	if plan := h.mustAssemble(Request{}); plan.Settings.Memory != "10G" {
		t.Fatalf("the instance's own memory beats everything: %+v", plan.Settings)
	}
}

func TestAssembleLosesOnlyTheClientMemoryWhenAPackCantBeRead(t *testing.T) {
	h := newHarness(t, "")
	h.editManifest(func(m *manifest.Manifest) {
		m.Requires["gone"] = manifest.Require{Type: "modpack", Source: filepath.Join(t.TempDir(), "gone")}
	})

	plan := h.mustAssemble(Request{})

	if plan.Settings.Memory != instance.DefaultMemory || !h.warned("client.memory") {
		t.Fatalf("a pack that can't be read costs the launch its client.memory, not the launch: %+v\n%v", plan.Settings, h.env.Warnings)
	}
}

func TestAssembleRefusesAWorldTheVersionCantBootInto(t *testing.T) {
	h := newHarness(t, "")

	_, err := h.assemble(Request{Target: game.QuickPlay{World: "New World"}})

	if err == nil || out.CodeOf(err) == "" {
		t.Fatalf("a version without quick play refuses --world before anything starts: %v", err)
	}
}

func TestLaunchTemplatesTheAccountAndSettingsIntoTheArgv(t *testing.T) {
	h := newHarness(t, "")
	h.editInstance(func(f *instance.File) {
		f.Settings.Memory, f.Settings.Window, f.Settings.Wrapper = "2G", "800x600", []string{"gamemoderun"}
	})
	plan := h.mustAssemble(Request{})

	launch, err := plan.Launch(h.e, offlineAccount("Steve", steveID), "", time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}

	argv := strings.Join(launch.Argv, " ")
	for _, want := range []string{"--username Steve", "--uuid " + steveID, "-Xmx2G", "--width 800 --height 600", "--gameDir " + h.in.Dir, "net.minecraft.client.main.Main"} {
		if !strings.Contains(argv, want) {
			t.Fatalf("argv is missing %q:\n%s", want, argv)
		}
	}
	if launch.Java != plan.Java || launch.Dir != h.in.Dir || len(launch.Wrapper) != 1 || launch.Wrapper[0] != "gamemoderun" {
		t.Fatalf("launch %+v", launch)
	}
	if launch.Log != filepath.Join(h.in.Dir, instance.Dir, "logs", "20260926-100000.log") {
		t.Fatalf("log %q", launch.Log)
	}

	sized, err := plan.Launch(h.e, offlineAccount("Steve", steveID), "1280x720", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if argv := strings.Join(sized.Argv, " "); !strings.Contains(argv, "--width 1280 --height 720") {
		t.Fatalf("--window overrides the setting for one run:\n%s", argv)
	}
}
