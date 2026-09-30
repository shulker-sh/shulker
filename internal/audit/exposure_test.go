package audit

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/modpack"
	"shulker.sh/shulker/internal/project"
)

const friends = "https://git.example/friends.git"

// exposureFixture adds to newFixture a pinned sodium, a git-source modpack that brings create and
// lays a config file, a hosted modpack that brings iris, and a local file, and returns the builder
// over it with the git modpack read.
func exposureFixture(t *testing.T) (*fixture, *build.Builder) {
	f := newFixture(t)
	sodium := f.p.Manifest.Requires["sodium"]
	sodium.Pin = "1.0"
	f.p.Manifest.Requires["sodium"] = sodium
	f.p.Manifest.Requires["friends"] = manifest.Require{Source: friends}
	f.p.Manifest.Requires["optimized"] = manifest.Require{Provider: "modrinth", Project: "optimized"}
	f.p.Manifest.Requires["handmade"] = manifest.Require{File: "jars/handmade.jar"}
	f.p.Lock.Modpacks["friends"] = lock.Modpack{Source: friends, Commit: "0123456789abcdef"}
	f.p.Lock.Modpacks["optimized"] = lock.Modpack{Provider: "modrinth", Project: "optimized-id", Version: "v1", VersionNumber: "1.0"}
	f.p.Lock.Mods["create"] = lock.Mod{Provider: "modrinth", Project: "create-id", Filename: "create.jar", Sha512: "aa", Side: "both", Modpack: "friends", RequiredBy: []string{}, Aliases: lock.Aliases{}}
	f.p.Lock.Mods["iris"] = lock.Mod{Provider: "modrinth", Project: "iris-id", Filename: "iris.jar", Sha512: "bb", Side: "both", Modpack: "optimized", RequiredBy: []string{}, Aliases: lock.Aliases{}}
	f.p.Lock.Mods["handmade"] = lock.Mod{File: "jars/handmade.jar", Filename: "handmade.jar", Sha512: "cc", Side: "both", RequiredBy: []string{}, Aliases: lock.Aliases{}}

	checkout := t.TempDir()
	if err := os.MkdirAll(filepath.Join(checkout, "overrides", "config"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(checkout, "overrides", "config", "create.toml"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.write("overrides/options.txt", []byte("fov:1"))
	packs := []*modpack.Loaded{{Name: "friends", Source: friends, Kind: modpack.Git, Dir: checkout, Manifest: &manifest.Manifest{Name: "friends", Client: &manifest.Client{}}}}
	return f, build.New(f.e.Env, f.p, packs)
}

// linkInPlace registers the fixture's directory as a Prism instance with its hooks as given.
func linkInPlace(t *testing.T, f *fixture, preLaunch bool, settings instance.LaunchSettings) project.InstanceEntry {
	file := instance.New()
	if !preLaunch {
		file.Settings.Hooks.PreLaunch = instance.Off()
	}
	file.Settings.LaunchSettings = settings
	if err := file.Save(f.p.Dir); err != nil {
		t.Fatal(err)
	}
	return project.InstanceEntry{Instance: config.Instance{ID: "pack", Launcher: "prism", Dir: f.p.Dir, Source: f.p.Dir}}
}

func expose(t *testing.T, b *build.Builder, o ExposureOptions) *Exposure {
	t.Helper()
	x, err := Expose(b, o)
	if err != nil {
		t.Fatal(err)
	}
	return x
}

func controlOf(t *testing.T, x *Exposure, key string) Control {
	t.Helper()
	i := slices.IndexFunc(x.Entries, func(e Exposed) bool { return e.Key == key })
	if i < 0 {
		t.Fatalf("no entry %s in %+v", key, x.Entries)
	}
	return x.Entries[i].Control
}

func TestExposureNamesWhoControlsEachEntryAndHowItChanges(t *testing.T) {
	f, b := exposureFixture(t)
	x := expose(t, b, ExposureOptions{Instances: []project.InstanceEntry{linkInPlace(t, f, true, instance.LaunchSettings{})}})

	followsFriends := Control{Owner: OwnerSource, Source: friends, Changes: Follows, AtLaunch: true}
	optimized := Control{Owner: OwnerProvider, Provider: "modrinth", Project: "optimized-id", Changes: Floating}
	want := map[string]Control{
		"friends":   followsFriends,
		"create":    followsFriends,
		"optimized": optimized,
		"iris":      optimized,
		"sodium":    {Owner: OwnerProvider, Provider: "modrinth", Project: "sodium-id", Changes: Pinned},
		"lithium":   {Owner: OwnerProvider, Provider: "modrinth", Project: "lithium-id", Changes: Floating},
		"handmade":  {Owner: OwnerPlayer, Changes: Local},
	}
	if len(x.Entries) != len(want) {
		t.Fatalf("entries: %+v", x.Entries)
	}
	for key, c := range want {
		if got := controlOf(t, x, key); got != c {
			t.Errorf("%s: got %+v, want %+v", key, got, c)
		}
	}
	if len(x.Launches) != 1 || x.Launches[0].Instance != "pack" || x.Launches[0].Launcher != "prism" || x.Launches[0].SyncsBy != ByHook {
		t.Fatalf("launches: %+v", x.Launches)
	}
}

func TestNothingChangesAtLaunchWithThePreLaunchHookOff(t *testing.T) {
	f, b := exposureFixture(t)
	x := expose(t, b, ExposureOptions{Instances: []project.InstanceEntry{linkInPlace(t, f, false, instance.LaunchSettings{})}})
	for _, e := range x.Entries {
		if e.AtLaunch {
			t.Errorf("%s changes at launch with the hook off", e.Key)
		}
	}
	if c := controlOf(t, x, "friends"); c.Changes != Follows {
		t.Fatalf("friends still follows its source on sync: %+v", c)
	}
	if x.Launches[0].SyncsBy != "" {
		t.Fatalf("launches: %+v", x.Launches)
	}
}

func TestAModpackThatDoesntAutoUpdateFloats(t *testing.T) {
	f, b := exposureFixture(t)
	off := false
	f.p.Manifest.Requires["friends"] = manifest.Require{Source: friends, AutoUpdate: &off}
	x := expose(t, b, ExposureOptions{Instances: []project.InstanceEntry{linkInPlace(t, f, true, instance.LaunchSettings{})}})
	want := Control{Owner: OwnerSource, Source: friends, Changes: Floating}
	if got := controlOf(t, x, "create"); got != want {
		t.Fatalf("create: %+v", got)
	}
}

func TestASyncedInstanceIsItsSourcesThroughout(t *testing.T) {
	f, b := exposureFixture(t)
	entry := linkInPlace(t, f, true, instance.LaunchSettings{})
	entry.Dir, entry.Source, entry.Launcher = t.TempDir(), "https://git.example/pack.git", "mojang"
	file := instance.New()
	if err := file.Save(entry.Dir); err != nil {
		t.Fatal(err)
	}
	x := expose(t, b, ExposureOptions{Instances: []project.InstanceEntry{entry}})
	want := Control{Owner: OwnerSource, Source: entry.Source, Changes: Follows, AtLaunch: true}
	for _, e := range x.Entries {
		if e.Control != want {
			t.Errorf("%s: %+v", e.Key, e.Control)
		}
	}
	for _, o := range x.Overrides {
		if o.Control != want {
			t.Errorf("%s: %+v", o.Folder, o.Control)
		}
	}
	if x.Launches[0].SyncsBy != ByShim {
		t.Fatalf("launches: %+v", x.Launches)
	}
}

func TestAnInstanceLinkedFromALocalProjectIsThePlayers(t *testing.T) {
	f, b := exposureFixture(t)
	entry := linkInPlace(t, f, true, instance.LaunchSettings{})
	entry.Dir = t.TempDir()
	if err := instance.New().Save(entry.Dir); err != nil {
		t.Fatal(err)
	}
	x := expose(t, b, ExposureOptions{Instances: []project.InstanceEntry{entry}})
	if got := controlOf(t, x, "handmade"); got != (Control{Owner: OwnerPlayer, Changes: Local}) {
		t.Fatalf("handmade: %+v", got)
	}
	if got := controlOf(t, x, "create"); got != (Control{Owner: OwnerSource, Source: friends, Changes: Follows, AtLaunch: true}) {
		t.Fatalf("create: %+v", got)
	}
}

func TestExposureListsOverrideFoldersAndTheFilesTheyLay(t *testing.T) {
	f, b := exposureFixture(t)
	x := expose(t, b, ExposureOptions{Instances: []project.InstanceEntry{linkInPlace(t, f, true, instance.LaunchSettings{})}})
	want := []OverrideFolder{
		{Folder: "friends:overrides", Modpack: "friends", Control: Control{Owner: OwnerSource, Source: friends, Changes: Follows, AtLaunch: true}, Files: []string{"config/create.toml"}},
		{Folder: "overrides", Control: Control{Owner: OwnerPlayer, Changes: Local}, Files: []string{"options.txt"}},
	}
	if len(x.Overrides) != len(want) {
		t.Fatalf("overrides: %+v", x.Overrides)
	}
	for i := range want {
		if got := x.Overrides[i]; got.Folder != want[i].Folder || got.Modpack != want[i].Modpack || got.Control != want[i].Control || !slices.Equal(got.Files, want[i].Files) {
			t.Errorf("override %d: got %+v, want %+v", i, got, want[i])
		}
	}
}

func TestLaunchSettingsSayWhereEachComesFrom(t *testing.T) {
	f, b := exposureFixture(t)
	entry := linkInPlace(t, f, true, instance.LaunchSettings{JVMArgs: []string{"-Xss4M"}})
	x := expose(t, b, ExposureOptions{Instances: []project.InstanceEntry{entry}, Play: instance.LaunchSettings{JVMArgs: []string{"-XX:+UseZGC"}, Wrapper: []string{"gamemoderun"}}, PackMemory: "6G"})
	got := map[string]string{}
	for _, s := range x.Launches[0].Settings {
		got[s.Key] = s.From
	}
	want := map[string]string{"memory": SetInPack, "jvmArgs": SetInInstance, "wrapper": SetInConfig}
	for key, from := range want {
		if got[key] != from {
			t.Errorf("%s: from %q, want %q", key, got[key], from)
		}
	}
}

func TestAProjectNoInstanceBuildsHasNoLaunch(t *testing.T) {
	_, b := exposureFixture(t)
	x := expose(t, b, ExposureOptions{})
	if len(x.Launches) != 0 || controlOf(t, x, "friends").AtLaunch {
		t.Fatalf("launches: %+v", x.Launches)
	}
}

func TestEveryInstanceTheProjectBuildsIsReported(t *testing.T) {
	f, b := exposureFixture(t)
	inPlace := linkInPlace(t, f, false, instance.LaunchSettings{Memory: "8G"})
	built := filepath.Join(f.p.Dir, "build", "client")
	client := project.InstanceEntry{Instance: config.Instance{ID: "pack-client", Launcher: "mojang", Dir: built, Source: f.p.Dir}}
	if err := instance.New().Save(built); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(instance.StatePath(built), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	stranger := project.InstanceEntry{Instance: config.Instance{ID: "other", Dir: t.TempDir()}}

	found := InstancesOf(b, []project.InstanceEntry{inPlace, stranger, client})
	if len(found) != 2 || found[0].ID != "pack" || found[1].ID != "pack-client" {
		t.Fatalf("instances: %+v", found)
	}
	x := expose(t, b, ExposureOptions{Instances: found})
	if len(x.Launches) != 2 || x.Launches[0].SyncsBy != "" || x.Launches[1].SyncsBy != ByShim {
		t.Fatalf("launches: %+v", x.Launches)
	}
	if x.Launches[0].Settings[0].Value != "8G" || x.Launches[1].Settings[0].From != SetByDefault {
		t.Fatalf("each instance has its own settings: %+v", x.Launches)
	}
	if c := controlOf(t, x, "create"); !c.AtLaunch {
		t.Fatalf("create changes when the instance that syncs launches: %+v", c)
	}
}
