package sync

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/local"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
)

func TestRunBuildsASideIntoADirectoryAndRecordsIt(t *testing.T) {
	h := newHarness(t)
	h.add("sodium")
	into := filepath.Join(t.TempDir(), "instance")

	res := h.mustSync(into, Request{})

	if res.Side != "client" || res.Dir != into || res.Kind != "local" || res.Source != h.dir || res.Project == nil {
		t.Fatalf("result %+v", res)
	}
	if !exists(h.modPath(into)) {
		t.Fatalf("the locked mod should be in %s", into)
	}
	if st := instance.LoadState(into); st.Origin != (instance.Origin{Source: h.dir}) || st.Side != "client" {
		t.Fatalf("state: %+v", st)
	}
	if lf, err := local.Load(h.dir); err != nil || len(lf.ExistingSyncDirs("client")) != 1 {
		t.Fatalf("the project's local file records where it was synced to: %+v %v", lf, err)
	}
	if f, err := instance.Load(into); err != nil || f.Source != h.dir || f.Side != "client" {
		t.Fatalf("the directory's instance file records its source: %+v %v", f, err)
	}
	if rows := h.instances(); len(rows) != 0 {
		t.Fatalf("a sync registers nothing: %+v", rows)
	}
}

func TestRunReadsTheInstancesFeatureDecisionOverTheProjects(t *testing.T) {
	h := newHarness(t)
	h.add("sodium")
	h.editManifest(func(m *manifest.Manifest) {
		m.Features = map[string]manifest.Feature{"fancy": {}}
		r := m.Requires["sodium"]
		r.Feature = manifest.StringList{"fancy"}
		m.Requires["sodium"] = r
	})
	lf, err := local.Load(h.dir)
	if err != nil {
		t.Fatal(err)
	}
	lf.SetFeature("fancy", true)
	if err := lf.Save(); err != nil {
		t.Fatal(err)
	}
	into := filepath.Join(t.TempDir(), "instance")
	writeFile(t, filepath.Join(into, local.FileName), `{"$schema":"https://shulker.sh/schema/v1/local.json","features":{"fancy":false}}`)

	res := h.mustSync(into, Request{})

	if len(res.Build.Excluded) != 1 || res.Build.Excluded[0] != "sodium (needs feature fancy)" {
		t.Fatalf("the instance decision should beat the project one: %+v", res.Build)
	}
	if st := instance.LoadState(into); st.Origin != (instance.Origin{Source: h.dir}) {
		t.Fatalf("state origin: %+v", st.Origin)
	}
}

func TestRunLeavesAnUnchangedLocalFileAlone(t *testing.T) {
	h := newHarness(t)
	h.add("sodium")
	into := filepath.Join(t.TempDir(), "one")
	h.mustSync(into, Request{})

	localPath := filepath.Join(h.dir, local.FileName)
	if err := os.Chmod(localPath, 0o444); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(localPath, 0o644) })
	h.mustSync(into, Request{})
	if h.warned("not updated") {
		t.Fatalf("an unchanged local file must not be rewritten: %v", h.env.Warnings)
	}
	two := filepath.Join(t.TempDir(), "two")
	h.mustSync(two, Request{})
	if h.warned("not updated") {
		t.Fatalf("a source project nobody can write to must not warn every sync: %v", h.env.Warnings)
	}
}

func TestRecordedRebuildsFromTheDirectorysOwnRecord(t *testing.T) {
	h := newHarness(t)
	h.add("sodium")
	into := filepath.Join(t.TempDir(), "instance")
	h.mustSync(into, Request{})

	res, err := Recorded(context.Background(), h.e, Request{Into: into})
	if err != nil || res.Build.Unchanged == 0 || len(res.Build.Written) != 0 {
		t.Fatalf("a re-sync rebuilds from the recorded source: %+v %v", res.Build, err)
	}
	if rows := h.instances(); len(rows) != 0 {
		t.Fatalf("a re-sync of a detached build registers nothing: %+v", rows)
	}
	_, err = Recorded(context.Background(), h.e, Request{Into: t.TempDir()})
	if out.CodeOf(err) != "source-unknown" {
		t.Fatalf("a directory with no record: %v", err)
	}
}

// Syncing a project into its own directory is a build of it: the directory is neither recorded
// as one the project was synced into nor made an instance.
func TestRunIntoTheProjectsOwnDirectoryIsABuild(t *testing.T) {
	h := newHarness(t)
	h.editManifest(func(m *manifest.Manifest) { m.Client.Build = "." })
	h.add("sodium")

	res := h.mustSync(h.dir, Request{})

	if res.Dir != h.dir || !exists(h.modPath(h.dir)) {
		t.Fatalf("the project directory should hold the build: %+v", res)
	}
	if lf, err := local.Load(h.dir); err != nil || len(lf.SyncDirs) != 0 {
		t.Fatalf("its own directory is no sync directory: %+v %v", lf, err)
	}
	if exists(instance.Path(h.dir)) {
		t.Fatal("its own directory takes no instance file")
	}
	if rows := h.instances(); len(rows) != 0 {
		t.Fatalf("its own directory is not registered: %+v", rows)
	}
}

func TestInstanceSyncsAnInPlaceProjectWhereItStands(t *testing.T) {
	h := newHarness(t)
	h.editManifest(func(m *manifest.Manifest) { m.Client.Build = "." })
	h.mustSync(h.dir, Request{})
	h.register(config.Instance{ID: "self", Name: "self", Dir: h.dir, Source: h.dir})
	h.add("sodium")

	res, err := Instance(context.Background(), h.e, project.Inspect(h.instances()[0]), Request{Reason: "sync"})
	if err != nil {
		t.Fatalf("sync -i: %v", err)
	}
	if res.Dir != h.dir || !exists(h.modPath(h.dir)) {
		t.Fatalf("the instance should be built in place: %+v", res)
	}
	if res.Relock == nil {
		t.Fatal("an in-place sync relocks first")
	}
	if rows := h.instances(); rows[0].LastSync == "" || rows[0].LastError != "" {
		t.Fatalf("the row records the sync: %+v", rows[0])
	}
}

func TestForLaunchKeepsThePlayersSideOfAConflict(t *testing.T) {
	h := newHarness(t)
	writeFile(t, filepath.Join(h.dir, "overrides", "options.txt"), "renderDistance:8\n")
	into := filepath.Join(t.TempDir(), "instance")
	h.mustSync(into, Request{})

	writeFile(t, filepath.Join(into, "options.txt"), "renderDistance:16\n")
	writeFile(t, filepath.Join(h.dir, "overrides", "options.txt"), "renderDistance:32\n")
	h.add("sodium")

	if _, err := h.sync(into, Request{}); out.CodeOf(err) != "build-conflict" {
		t.Fatalf("an explicit sync still fails on a conflict: %v", err)
	}
	res, err := ForLaunch(context.Background(), h.e, into, "hook")
	if err != nil {
		t.Fatalf("a launch-time sync goes on past a conflict: %v", err)
	}
	if len(res.Build.KeptConflicts) != 1 || res.Build.KeptConflicts[0] != "options.txt (changed in place and in the source)" {
		t.Fatalf("kept conflicts: %v", res.Build.KeptConflicts)
	}
	if got := readFile(t, filepath.Join(into, "options.txt")); got != "renderDistance:16\n" {
		t.Fatalf("options.txt = %q", got)
	}
	if !exists(h.modPath(into)) {
		t.Fatal("the rest of the update applies")
	}
	res, err = ForLaunch(context.Background(), h.e, into, "hook")
	if err != nil || len(res.Build.KeptConflicts) != 0 {
		t.Fatalf("a kept file is edited in place from then on: %+v %v", res.Build, err)
	}
}

func TestFeatureOverridesRefusesNamesTheBuildDoesNotKnow(t *testing.T) {
	h := newHarness(t)
	h.add("sodium")
	h.editManifest(func(m *manifest.Manifest) {
		m.Features = map[string]manifest.Feature{"fancy": {}}
		r := m.Requires["sodium"]
		r.Feature = manifest.StringList{"fancy"}
		m.Requires["sodium"] = r
	})

	if _, err := h.sync(filepath.Join(t.TempDir(), "a"), Request{With: []string{"fancy"}, Without: []string{"fancy"}}); out.CodeOf(err) != "usage" {
		t.Fatalf("--with and --without both naming a feature: %v", err)
	}
	_, err := h.sync(filepath.Join(t.TempDir(), "b"), Request{Without: []string{"plain"}})
	if fail := out.AsError(err); fail.Code != "feature-not-found" || len(fail.Candidates) != 1 || fail.Candidates[0] != "fancy" {
		t.Fatalf("an unknown feature names the known ones: %+v", fail)
	}
	res := h.mustSync(filepath.Join(t.TempDir(), "c"), Request{Without: []string{"fancy"}})
	if len(res.Build.Excluded) != 1 {
		t.Fatalf("--without turns the feature off for the run: %+v", res.Build)
	}
}
