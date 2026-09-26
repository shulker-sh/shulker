package link

import (
	"path/filepath"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/sync"
)

func TestIntoShulkerMakesAnInstanceThatFollowsTheSourceAndBuildsIt(t *testing.T) {
	h := newHarness(t)

	rep := h.mustLink(launcher.Shulker, h.dir, Request{})

	gameDir := filepath.Join(h.root, "pack")
	if !rep.Created || rep.ID != "pack" || rep.Shown != "pack" || rep.Noun != "instance" || rep.GameDir != gameDir || rep.InstanceDir != gameDir {
		t.Fatalf("report %+v", rep)
	}
	if rep.Modpack != "pack" || rep.Source != h.dir || rep.Command != "" || rep.LauncherDir != "" || rep.Sync.Dir != gameDir || rep.Sync.Build == nil {
		t.Fatalf("every link reports the pack it follows and the build it did: %+v", rep)
	}
	m := h.manifest(gameDir)
	if m.Name != "pack" || m.Client == nil || m.Client.Build != "." {
		t.Fatalf("a shulker instance is a project built in place: %+v", m)
	}
	if len(m.Requires) != 1 || m.Requires["pack"].Source != h.dir {
		t.Fatalf("a link writes exactly one modpack entry: %v", m.Requires)
	}
	if !exists(filepath.Join(gameDir, "mods", h.sodium.File.Filename)) {
		t.Fatal("a link builds the instance before it returns")
	}
	if exists(filepath.Join(h.dir, "build")) {
		t.Fatal("a link must not build in the source project")
	}
	rows := h.instances()
	want := config.Instance{ID: "pack", Name: "pack", Launcher: "shulker", Dir: gameDir, Source: h.dir}
	if len(rows) != 1 || rows[0].LastSync == "" || rows[0].LastError != "" {
		t.Fatalf("the sync stamps the row: %+v", rows)
	}
	rows[0].LastSync = ""
	if rows[0] != want {
		t.Fatalf("registry = %+v, want %+v", rows[0], want)
	}
	for _, kind := range []launcher.HookKind{launcher.HookPreLaunch, launcher.HookPostExit} {
		if exists(filepath.Join(gameDir, instance.Dir, string(kind))) {
			t.Fatalf("a shulker instance needs no %s script", kind)
		}
	}
	if h.warned("not in the lock") {
		t.Fatalf("a fresh link has no lock to be missing from: %v", h.env.Warnings)
	}

	again := h.mustLink(launcher.Shulker, h.dir, Request{})
	if again.Created || again.ID != "pack" || len(h.instances()) != 1 {
		t.Fatalf("a relink updates the instance it made: %+v", again)
	}
}

func TestIntoShulkerTakesTheNicknameAsTheFolderAndTheID(t *testing.T) {
	h := newHarness(t)

	h.mustLink(launcher.Shulker, h.dir, Request{ID: "smp"})
	if !exists(filepath.Join(h.root, "smp", manifest.FileName)) {
		t.Fatal("--as names the folder under the instances root")
	}
	if rows := h.instances(); len(rows) != 1 || rows[0].ID != "smp" {
		t.Fatalf("--as names the row too: %+v", rows)
	}

	// A second source under the same nickname is a different instance asking for one folder.
	other := h.newSource("pack", "lithium")
	if _, err := h.link(launcher.Shulker, other, Request{ID: "smp"}); out.CodeOf(err) != "instance-exists" {
		t.Fatalf("err %v", err)
	}
	h.mustLink(launcher.Shulker, other, Request{})
	if !exists(filepath.Join(h.root, "pack", manifest.FileName)) || !exists(filepath.Join(h.root, "smp", manifest.FileName)) {
		t.Fatal("without --as the pack's own name is free, and the refused link leaves the first instance alone")
	}

	for _, bad := range []string{"Bad Name", "-"} {
		if _, err := h.link(launcher.Shulker, other, Request{ID: bad}); out.CodeOf(err) != "usage" {
			t.Fatalf("--as %q: %v", bad, err)
		}
	}
}

func TestIntoRefusesAnIDAnotherInstanceHolds(t *testing.T) {
	h := newHarness(t)
	h.mustLink(launcher.Shulker, h.dir, Request{ID: "smp"})

	_, err := h.link(launcher.Find("prism"), h.dir, Request{LauncherDir: t.TempDir(), ID: "smp"})

	if out.CodeOf(err) != "instance-id-taken" {
		t.Fatalf("err %v", err)
	}
}

func TestIntoForceRepointsTheModpackAndKeepsThePlayersEntries(t *testing.T) {
	h := newHarness(t)
	rep := h.mustLink(launcher.Shulker, h.dir, Request{})
	h.add(rep.GameDir, "lithium")
	other := h.newSource("other", "")

	if _, err := h.link(launcher.Shulker, other, Request{ID: "pack"}); out.CodeOf(err) != "instance-exists" {
		t.Fatalf("an instance following another pack is refused: %v", err)
	}
	h.mustLink(launcher.Shulker, other, Request{ID: "pack", Force: true})

	m := h.manifest(rep.GameDir)
	if _, kept := m.Requires["lithium"]; len(m.Requires) != 2 || m.Requires["pack"].Source != other || !kept {
		t.Fatalf("--force repoints the pack and keeps the player's own entries: %v", m.Requires)
	}
	if !exists(filepath.Join(rep.GameDir, "mods", h.lithium.File.Filename)) {
		t.Fatal("what the player added stays")
	}
}

// A pack's retention and its marker are copied once, because the author is the one who knows how
// big a build is and whether the mod list has to match exactly. From then on both are the player's.
func TestIntoCopiesThePacksPreferencesOnce(t *testing.T) {
	h := newHarness(t)
	two, off := 2, false
	h.editManifest(h.dir, func(m *manifest.Manifest) { m.History, m.Marker = &two, &off })

	rep := h.mustLink(launcher.Shulker, h.dir, Request{})

	m := h.manifest(rep.GameDir)
	if m.History == nil || *m.History != 2 || m.Marker == nil || *m.Marker {
		t.Fatalf("the pack's retention and marker should be copied into the instance: history=%v marker=%v", m.History, m.Marker)
	}
	if exists(filepath.Join(rep.GameDir, "mods", "shulker-pack.jar")) {
		t.Fatal("a pack that leaves the marker out gives an instance that leaves it out")
	}
}

func TestIntoPrismWritesTheLauncherFilesAndFillsTheSlot(t *testing.T) {
	h := newHarness(t)
	launcherDir := t.TempDir()

	rep := h.mustLink(launcher.Find("prism"), h.dir, Request{LauncherDir: launcherDir})

	instDir := filepath.Join(launcherDir, "instances", "shulker-pack")
	gameDir := filepath.Join(instDir, "minecraft")
	wantCmd := `sh "$INST_MC_DIR/.shulker/pre-launch"`
	if rep.Launcher != "prism" || rep.Instance != "shulker-pack" || rep.InstanceDir != instDir || rep.GameDir != gameDir || !rep.Created || rep.LauncherDir != launcherDir {
		t.Fatalf("report %+v", rep)
	}
	if rep.Command != wantCmd || rep.Shown != "pack" || rep.Modpack != "pack" || rep.Sync.Dir != gameDir {
		t.Fatalf("report %+v", rep)
	}
	cfg := readINI(t, filepath.Join(instDir, launcher.PrismInstanceFile))
	if cfg["name"] != "pack" || cfg["PreLaunchCommand"] != wantCmd {
		t.Fatalf("instance.cfg: %v", cfg)
	}
	if !exists(filepath.Join(gameDir, instance.Dir, string(launcher.HookPreLaunch))) {
		t.Fatal("a launcher with a slot gets its generated script")
	}
	if !exists(filepath.Join(gameDir, "mods", h.sodium.File.Filename)) {
		t.Fatal("a link builds the instance before it returns")
	}
	if rows := h.instances(); len(rows) != 1 || rows[0].LauncherDir != launcherDir || rows[0].Launcher != "prism" || rows[0].ID != "pack" {
		t.Fatalf("registry %+v", rows)
	}

	// A mod added in the game directory sits on top of the pack, and a relink keeps it.
	h.add(gameDir, "lithium")
	again := h.mustLink(launcher.Find("prism"), h.dir, Request{LauncherDir: launcherDir})
	if again.Created {
		t.Fatalf("a relink updates: %+v", again)
	}
	for _, jar := range []string{h.sodium.File.Filename, h.lithium.File.Filename} {
		if !exists(filepath.Join(gameDir, "mods", jar)) {
			t.Fatalf("a relink keeps %s", jar)
		}
	}
}

func TestIntoSeedsHooksFromTheManifestThenTheSettings(t *testing.T) {
	h := newHarness(t)
	off := false
	h.editManifest(h.dir, func(m *manifest.Manifest) {
		m.Marker = &off
		m.Client.Hooks = &manifest.Hooks{PostExit: &off}
	})
	prism := launcher.Find("prism")

	first := t.TempDir()
	rep := h.mustLink(prism, h.dir, Request{LauncherDir: first})
	f, err := instance.Load(rep.GameDir)
	if err != nil {
		t.Fatal(err)
	}
	if !f.Settings.PreLaunch() || f.Settings.PostExit() || f.Settings.Marker != nil {
		t.Fatalf("the hooks seed the instance and the marker is left to the manifest: %+v", f.Settings)
	}
	cfg := readINI(t, filepath.Join(rep.InstanceDir, launcher.PrismInstanceFile))
	if cfg["PreLaunchCommand"] == "" || cfg["PostExitCommand"] != "" {
		t.Fatalf("the switch that is off gets no slot: %+v", cfg)
	}
	if exists(filepath.Join(rep.GameDir, "mods", "shulker-"+rep.ID+".jar")) {
		t.Fatal("a pack that leaves the marker out gives an instance that leaves it out")
	}

	java := filepath.Join(h.dir, "jdk", "bin", "java")
	second := t.TempDir()
	rep = h.mustLink(prism, h.dir, Request{LauncherDir: second, Settings: Settings{NoPreLaunch: true, Marker: instance.Off(), Java: java, Wrapper: []string{"gamemoderun", "--dlsym"}}})
	if f, err = instance.Load(rep.GameDir); err != nil {
		t.Fatal(err)
	}
	if f.Settings.PreLaunch() || f.Settings.PostExit() || f.Settings.Marker == nil || *f.Settings.Marker {
		t.Fatalf("the settings turn pre-launch off over the manifest's own postExit, and write the marker out: %+v", f.Settings)
	}
	if f.Settings.Java != java || !slices.Equal(f.Settings.Wrapper, []string{"gamemoderun", "--dlsym"}) {
		t.Fatalf("java and wrapper land in the settings: %+v", f.Settings)
	}
	cfg = readINI(t, filepath.Join(rep.InstanceDir, launcher.PrismInstanceFile))
	if cfg["WrapperCommand"] != "gamemoderun --dlsym" || cfg["PreLaunchCommand"] != "" || cfg["PostExitCommand"] != "" {
		t.Fatalf("the wrapper goes in the launcher's own slot and neither command slot is filled: %+v", cfg)
	}

	// On a relink the block belongs to whoever edited it: nothing is reseeded.
	rep = h.mustLink(prism, h.dir, Request{LauncherDir: second})
	if f, err = instance.Load(rep.GameDir); err != nil {
		t.Fatal(err)
	}
	if f.Settings.PreLaunch() || f.Settings.Java != java {
		t.Fatalf("a relink leaves the settings alone: %+v", f.Settings)
	}

	third := t.TempDir()
	rep = h.mustLink(prism, h.dir, Request{LauncherDir: third, Settings: Settings{Marker: instance.On()}})
	if f, err = instance.Load(rep.GameDir); err != nil {
		t.Fatal(err)
	}
	if f.Settings.Marker == nil || !*f.Settings.Marker || !exists(filepath.Join(rep.GameDir, "mods", "shulker-"+rep.ID+".jar")) {
		t.Fatalf("marker on puts the jar back over a manifest that leaves it out: %+v", f.Settings)
	}
}

func TestIntoSavesFeatureChoicesAndRefusesUnknownOnes(t *testing.T) {
	h := newHarness(t)
	h.editManifest(h.dir, func(m *manifest.Manifest) {
		m.Features = map[string]manifest.Feature{"fancy": {}}
		r := m.Requires["sodium"]
		r.Feature = manifest.StringList{"fancy"}
		m.Requires["sodium"] = r
	})

	if _, err := h.link(launcher.Shulker, h.dir, Request{With: []string{"shiny"}}); err == nil || exists(filepath.Join(h.root, "pack")) {
		t.Fatalf("an unknown feature is refused before anything is written: %v", err)
	}

	rep := h.mustLink(launcher.Shulker, h.dir, Request{Without: []string{"fancy"}})
	if !rep.FeaturesSaved {
		t.Fatalf("report %+v", rep)
	}
	lf, err := sync.LoadLocal(h.e.Env, rep.GameDir)
	if err != nil {
		t.Fatal(err)
	}
	if on, decided := lf.Features["fancy"]; !decided || on {
		t.Fatalf("the choice is written into the instance's local file: %+v", lf.Features)
	}
	if exists(filepath.Join(rep.GameDir, "mods", h.sodium.File.Filename)) {
		t.Fatal("the build honours the choice")
	}
}

func TestIntoWarnsWhenTheSourceDeclaresNoClient(t *testing.T) {
	h := newHarness(t)
	h.editManifest(h.dir, func(m *manifest.Manifest) { m.Client, m.Server = nil, &manifest.Server{} })
	h.add(h.dir, "lithium")

	rep := h.mustLink(launcher.Shulker, h.dir, Request{})

	if !h.warned(NoClientPack) {
		t.Fatalf("warnings %v", h.env.Warnings)
	}
	if !exists(filepath.Join(rep.GameDir, "mods", h.lithium.File.Filename)) {
		t.Fatal("a both-side mod still reaches the client")
	}
}
