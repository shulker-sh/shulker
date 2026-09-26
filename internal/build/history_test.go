package build

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/fsutil"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/provider"
)

func TestHistoryLeavesPlacedDatapacksToTheCache(t *testing.T) {
	dir := t.TempDir()
	placed := []string{
		"datapacks/dp.zip",
		"config/paxi/datapacks/dp.zip",
		"config/openloader/data/dp.zip",
		"config/openloader/packs/dp.zip",
		"survival/datapacks/dp.zip",
		"other/datapacks/dp.zip",
		"config/openloader/data/mine.zip",
	}
	files := map[string]string{}
	for _, rel := range placed {
		files[rel] = "x"
		if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := fsutil.Write(filepath.Join(dir, rel), []byte("x")); err != nil {
			t.Fatal(err)
		}
	}
	if err := instance.WriteState(dir, instance.State{Files: files}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, PropertiesFile), []byte("level-name=survival\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	lk := &lock.Lock{Datapacks: map[string]lock.Pack{"dp": {Filename: "dp.zip"}}}
	cached := leftToCache(dir, lk)
	into := t.TempDir()
	if err := copyTree(dir, historyConfig, into, cached); err != nil {
		t.Fatal(err)
	}
	kept := managedFiles(dir, cached)
	for _, rel := range placed {
		if _, err := os.Stat(filepath.Join(into, rel)); err == nil {
			kept = append(kept, rel)
		}
	}
	if want := []string{"other/datapacks/dp.zip", "config/openloader/data/mine.zip"}; !slices.Equal(kept, want) {
		t.Fatalf("entry holds %v, want %v", kept, want)
	}
}

// inPlaceProject builds its client into the project dir itself, where a build keeps history.
func inPlaceProject(t *testing.T) *testProject {
	t.Helper()
	p := newProject(t)
	p.b.Manifest.Client.Build = "."
	return p
}

func (p *testProject) history() []HistoryEntry {
	p.t.Helper()
	entries, err := History(p.b.Dir)
	if err != nil {
		p.t.Fatal(err)
	}
	return entries
}

// kept reads rel out of the history entry id.
func (p *testProject) kept(id, rel string) string {
	data, _ := os.ReadFile(filepath.Join(HistoryPath(p.b.Dir), id, filepath.FromSlash(rel)))
	return string(data)
}

func TestHistoryKeepsEditsBeforeABuild(t *testing.T) {
	p := inPlaceProject(t)
	p.override("config/x.txt", "from the pack\n")
	p.mustBuild("client", Options{})
	if entries := p.history(); len(entries) != 0 {
		t.Fatalf("a first build with nothing of yours at risk keeps nothing: %+v", entries)
	}

	p.file("config/x.txt", "mine\n")
	report := p.mustBuild("client", Options{})
	entries := p.history()
	if len(entries) != 1 || entries[0].Reason != "build" || entries[0].Side != "client" || report.History != entries[0].ID {
		t.Fatalf("a build over an edit should keep it: %+v %q", entries, report.History)
	}
	if got := p.kept(entries[0].ID, "config/x.txt"); got != "mine\n" {
		t.Fatalf("the entry should hold the edit: %q", got)
	}
}

func TestForcedBuildKeepsTheDriftItOverwrites(t *testing.T) {
	p := inPlaceProject(t)
	p.override("config/x.txt", "from the pack\n")
	p.override("config/gone.txt", "from the pack\n")
	p.mustBuild("client", Options{})
	p.file("config/x.txt", "mine\n")
	p.file("config/gone.txt", "mine too\n")
	if err := os.Remove(filepath.Join(p.b.Dir, "overrides", "config", "gone.txt")); err != nil {
		t.Fatal(err)
	}

	p.mustBuild("client", Options{Force: true})
	if got := p.project("config/x.txt"); got != "from the pack\n" {
		t.Fatalf("force should overwrite the edit: %q", got)
	}
	if p.project("config/gone.txt") != "" {
		t.Fatal("force should remove the edited file the source dropped")
	}
	entries := p.history()
	if len(entries) != 1 || entries[0].Reason != "build" {
		t.Fatalf("a forced build over edits should keep them: %+v", entries)
	}
	if got := p.kept(entries[0].ID, "config/x.txt"); got != "mine\n" {
		t.Fatalf("the entry should hold the overwritten edit: %q", got)
	}
	if got := p.kept(entries[0].ID, "config/gone.txt"); got != "mine too\n" {
		t.Fatalf("the entry should hold the removed edit: %q", got)
	}
}

func TestForcedBuildKeepsAnOverwrittenOption(t *testing.T) {
	p := inPlaceProject(t)
	p.b.Manifest.Client.Options = map[string]any{"tutorialStep": "none"}
	p.mustBuild("client", Options{})
	p.file("options.txt", strings.Replace(p.project("options.txt"), "tutorialStep:none", "tutorialStep:movement", 1))

	p.mustBuild("client", Options{Force: true})
	if got := p.project("options.txt"); !strings.Contains(got, "tutorialStep:none") {
		t.Fatalf("force should overwrite the edited key: %s", got)
	}
	entries := p.history()
	if len(entries) != 1 {
		t.Fatalf("a forced build over an edited key should keep it: %+v", entries)
	}
	if got := p.kept(entries[0].ID, "options.txt"); !strings.Contains(got, "tutorialStep:movement") {
		t.Fatalf("the entry should hold the edited key: %s", got)
	}
}

func TestHistoryWarnsPastTheKeptCountAndPruneTrimsIt(t *testing.T) {
	p := inPlaceProject(t)
	one := 1
	p.b.Manifest.History = &one
	p.override("config/x.txt", "from the pack\n")
	p.mustBuild("client", Options{})

	p.file("config/x.txt", "mine\n")
	if report := p.mustBuild("client", Options{Force: true}); contains(report.Warnings, "history entries are kept") {
		t.Fatalf("the first entry is within the limit: %q", report.Warnings)
	}
	p.file("config/x.txt", "mine again\n")
	report := p.mustBuild("client", Options{Force: true})
	if !slices.Contains(report.Warnings, "2 history entries are kept; `shulker history prune` trims them to 1") {
		t.Fatalf("a change over the limit should warn: %q", report.Warnings)
	}
	before := p.history()
	if len(before) != 2 {
		t.Fatalf("nothing but prune deletes: %+v", before)
	}

	dropped, err := PruneHistory(p.b.Dir, 1)
	if err != nil || len(dropped) != 1 || dropped[0].ID != before[1].ID {
		t.Fatalf("prune drops the oldest: %+v %v", dropped, err)
	}
	if after := p.history(); len(after) != 1 || after[0].ID != before[0].ID {
		t.Fatalf("prune should leave the newest: %+v", after)
	}
}

func TestRestoreHistoryPutsTheKeptStateBack(t *testing.T) {
	p := inPlaceProject(t)
	p.override("config/x.txt", "from the pack\n")
	p.mustBuild("client", Options{})
	p.file("config/x.txt", "mine\n")
	p.mustBuild("client", Options{})

	p.b.Manifest.Description = "changed later"
	p.override("config/x.txt", "v2\n")
	p.mustBuild("client", Options{Force: true})
	if got := p.project("config/x.txt"); got != "v2\n" {
		t.Fatalf("the forced build wrote the new source: %q", got)
	}

	older, err := PickHistory(p.b.Dir, 2)
	if err != nil {
		t.Fatal(err)
	}
	if err := RestoreHistory(p.b.Dir, older); err != nil {
		t.Fatal(err)
	}
	if got := p.project("config/x.txt"); got != "mine\n" {
		t.Fatalf("the config folder comes back whole: %q", got)
	}
	if strings.Contains(p.project("shulker.json"), "changed later") {
		t.Fatal("the manifest comes back as it was")
	}
	if _, err := PickHistory(p.b.Dir, 3); out.CodeOf(err) != "history-missing" {
		t.Fatalf("picking past the end: %v", err)
	}
}

func TestHistoryChangesReadAsWhatRestoringWouldDo(t *testing.T) {
	p := inPlaceProject(t)
	lockVersioned := func(key, version string) {
		v := provider.Version{ID: "m-" + key + "-" + version, Number: version, File: provider.File{Filename: key + "-" + version + ".jar"}}
		p.lockMod(key, p.modrinth, p.modrinth.Publish(mod(key+"-id", key), v, modJar(t, key, version)))
	}
	lockVersioned("sodium", "0.9")
	lockVersioned("iris", "3.0")
	bsl := provider.Version{ID: "m-bsl-8", Number: "8", File: provider.File{Filename: "bsl-8.zip"}}
	p.lockPack(manifest.TypeShader, "bsl", p.modrinth, p.modrinth.Publish(mod("bsl-id", "bsl"), bsl, packZip(t, "bsl")))
	p.save()
	e, err := TakeHistory(p.b.Dir, 5, HistoryEntry{Side: "client", Reason: "update"})
	if err != nil {
		t.Fatal(err)
	}
	delete(p.b.Lock.Mods, "iris")
	lockVersioned("sodium", "1.0")
	lockVersioned("lithium", "2.0")
	p.b.Lock.Shaders = nil

	changes, err := HistoryChanges(p.b.Dir, p.b.Lock, e)
	if err != nil {
		t.Fatal(err)
	}
	want := []HistoryChange{
		{Mod: "iris", To: "3.0"},
		{Mod: "lithium", From: "2.0"},
		{Mod: "sodium", From: "1.0", To: "0.9"},
		{Mod: "bsl", Kind: manifest.TypeShader, To: "8"},
	}
	if !slices.Equal(changes, want) {
		t.Fatalf("got %+v, want %+v", changes, want)
	}
	if err := os.Remove(filepath.Join(HistoryPath(p.b.Dir), e.ID, lock.FileName)); err != nil {
		t.Fatal(err)
	}
	if _, err := HistoryChanges(p.b.Dir, p.b.Lock, e); out.CodeOf(err) != "history-invalid" {
		t.Fatalf("an entry without its lock is history-invalid, got %v", err)
	}
}
