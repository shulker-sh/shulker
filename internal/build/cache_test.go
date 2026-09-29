package build

import (
	"crypto/sha512"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/instance"
	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/out"
	"shulker.sh/shulker/internal/project"
	"shulker.sh/shulker/internal/provider"
)

// withSodium is an in-place project with sodium locked and its jar in the cache.
func withSodium(t *testing.T) (*testProject, provider.Version) {
	t.Helper()
	p := inPlaceProject(t)
	sodium := p.modrinth.Publish(provider.Project{ID: "AANobbMI", Slug: "sodium", Title: "Sodium"}, provider.Version{ID: "QANobbMI", Number: "0.9.2", File: provider.File{Filename: "sodium-fabric-0.9.2+mc26.2.jar"}}, modJar(t, "sodium", "0.9.2"))
	p.lockMod("sodium", p.modrinth, sodium)
	p.save()
	return p, sodium
}

func registered(dir, source string) project.InstanceEntry {
	return project.InstanceEntry{Instance: config.Instance{ID: filepath.Base(dir), Launcher: "prism", Name: filepath.Base(dir), Dir: dir, Source: source}}
}

func mustRoots(t *testing.T, c *cache.Cache, instances []project.InstanceEntry, dir string, named ...string) Roots {
	t.Helper()
	r, err := CacheRoots(c, instances, dir, named)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// A history entry leaves mod files to the cache, so the objects its lock names are roots of
// their own: without them a rollback could not run offline.
func TestCacheRootsKeepTheProjectsHistoryEntries(t *testing.T) {
	p, sodium := withSodium(t)
	p.mustBuild("client", Options{})
	if _, err := TakeHistory(p.b.Dir, p.b.Manifest.HistoryKeep(), HistoryEntry{Side: "client", Reason: "remove"}); err != nil {
		t.Fatal(err)
	}
	delete(p.b.Manifest.Requires, "sodium")
	delete(p.b.Lock.Mods, "sodium")
	p.mustBuild("client", Options{})
	if entries := p.history(); len(entries) != 1 {
		t.Fatalf("the relock before the remove should keep an entry: %+v", entries)
	}

	r := mustRoots(t, p.b.Cache, nil, p.b.Dir)
	if !r.Project || r.Count() != 1 || len(r.Locks) != 2 || r.Instances != 0 {
		t.Fatalf("roots: %+v", r)
	}
	if _, err := r.Prune(p.b.Cache, cache.PruneOptions{}); err != nil {
		t.Fatal(err)
	}
	if !exists(p.b.Cache.Object(sodium.File.Sha512)) {
		t.Fatal("the entry taken before the remove still needs sodium")
	}
}

func TestCacheRootsFindNoProjectOutsideOne(t *testing.T) {
	c := &cache.Cache{Dir: t.TempDir()}
	r := mustRoots(t, c, nil, t.TempDir())
	if r.Project || r.Count() != 0 || len(r.Locks) != 0 {
		t.Fatalf("a folder that is no project roots nothing: %+v", r)
	}
}

// A registered instance that was deleted can need nothing, so it is skipped rather than
// blocking every prune until it is unlinked.
func TestCacheRootsSkipAGoneInstance(t *testing.T) {
	p, _ := withSodium(t)
	gone := filepath.Join(t.TempDir(), "deleted")
	r := mustRoots(t, p.b.Cache, []project.InstanceEntry{registered(gone, gone)}, p.b.Dir)
	if r.Instances != 0 || !r.Project || r.Count() != 1 {
		t.Fatalf("a gone instance should not count as a root: %+v", r)
	}
}

// An instance built into its own directory keeps its lock in the project it was built from, not
// in the game directory the registry records, so the roots follow the link's source.
func TestCacheRootsFollowASeparateDirInstanceToItsProject(t *testing.T) {
	p := newProject(t)
	sodium := p.modrinth.Publish(provider.Project{ID: "AANobbMI", Slug: "sodium", Title: "Sodium"}, provider.Version{ID: "QANobbMI", Number: "0.9.2", File: provider.File{Filename: "sodium-fabric-0.9.2+mc26.2.jar"}}, modJar(t, "sodium", "0.9.2"))
	p.lockMod("sodium", p.modrinth, sodium)
	p.mustBuild("client", Options{})
	built := p.builtPath("client", "")

	r := mustRoots(t, p.b.Cache, []project.InstanceEntry{registered(built, p.b.Dir)}, t.TempDir())
	if r.Instances != 1 || r.Project || len(r.Locks) != 1 {
		t.Fatalf("roots: %+v", r)
	}
	if _, err := r.Prune(p.b.Cache, cache.PruneOptions{}); err != nil {
		t.Fatal(err)
	}
	if !exists(p.b.Cache.Object(sodium.File.Sha512)) {
		t.Fatal("a registered instance still needs the mods its project locks")
	}
}

func TestCacheRootsCountARegisteredProjectOnce(t *testing.T) {
	p, _ := withSodium(t)
	p.save()
	r := mustRoots(t, p.b.Cache, []project.InstanceEntry{registered(p.b.Dir, p.b.Dir), registered(p.b.Dir, p.b.Dir)}, p.b.Dir)
	if r.Instances != 1 || r.Project || r.Count() != 1 || len(r.Locks) != 1 {
		t.Fatalf("a project registered as an instance is one root: %+v", r)
	}
}

// A directory synced from a git source runs on the lock of the checkout it was built from, so
// that checkout's lock is a root while the directory is registered.
func TestCacheRootsReadARemoteSourcesCheckoutLock(t *testing.T) {
	p, sodium := withSodium(t)
	c := p.b.Cache
	dir := t.TempDir()
	source := "https://example.com/pack.git"
	if err := instance.WriteState(dir, instance.State{Origin: instance.Origin{Source: source, Commit: "c0ffee"}}); err != nil {
		t.Fatal(err)
	}
	checkout := filepath.Join(c.PackSource("c0ffee"), lock.FileName)
	writeFile(t, checkout, p.project(lock.FileName))

	in := registered(dir, source)
	in.Ref = "main"
	r := mustRoots(t, c, []project.InstanceEntry{in}, t.TempDir())
	if r.Instances != 1 || len(r.Locks) != 1 || r.Locks[0].Source != source || r.Locks[0].Ref != "main" {
		t.Fatalf("roots: %+v", r)
	}
	if _, err := r.Prune(c, cache.PruneOptions{}); err != nil {
		t.Fatal(err)
	}
	if !exists(c.Object(sodium.File.Sha512)) {
		t.Fatal("what the checkout's lock names should survive")
	}
}

// A root that can't be read stops a prune, since it may be a live instance, but a dry run
// still reports.
func TestCacheRootsCarryAnUnreadableInstanceLock(t *testing.T) {
	c := &cache.Cache{Dir: t.TempDir()}
	broken := t.TempDir()
	writeFile(t, filepath.Join(broken, lock.FileName), "{ not a lock")

	r := mustRoots(t, c, []project.InstanceEntry{registered(broken, broken)}, t.TempDir())
	if r.Instances != 1 || len(r.Locks) != 0 || len(r.Unreadable) != 1 {
		t.Fatalf("roots: %+v", r)
	}
	if _, err := r.Prune(c, cache.PruneOptions{DryRun: true}); err != nil {
		t.Fatalf("a dry run reports past an unreadable root: %v", err)
	}
	_, err := r.Prune(c, cache.PruneOptions{})
	if e := out.AsError(err); err == nil || e.Code != "cache-root-unreadable" || e.Message != r.Unreadable[0] {
		t.Fatalf("a prune refuses an unreadable root: %v", err)
	}
}

// A CI runner registers no instances, so a repo holding several packs names each pack's lock
// to keep.
func TestCacheRootsCountNamedLocks(t *testing.T) {
	p, sodium := withSodium(t)
	p.save()
	named := filepath.Join(t.TempDir(), "other.lock")
	writeFile(t, named, p.project(lock.FileName))
	elsewhere := t.TempDir()

	r := mustRoots(t, p.b.Cache, nil, elsewhere, named)
	if r.LockFiles != 1 || r.Count() != 1 || r.Project || len(r.Locks) != 1 {
		t.Fatalf("a named lock should count as a root: %+v", r)
	}
	if _, err := r.Prune(p.b.Cache, cache.PruneOptions{}); err != nil {
		t.Fatal(err)
	}
	if !exists(p.b.Cache.Object(sodium.File.Sha512)) {
		t.Fatal("a named lock's mods must survive a prune")
	}
	if _, err := mustRoots(t, p.b.Cache, nil, elsewhere).Prune(p.b.Cache, cache.PruneOptions{}); err != nil {
		t.Fatal(err)
	}
	if exists(p.b.Cache.Object(sodium.File.Sha512)) {
		t.Fatal("without the lock nothing keeps sodium")
	}
}

func TestCacheRootsRefuseAMissingNamedLock(t *testing.T) {
	c := &cache.Cache{Dir: t.TempDir()}
	_, err := CacheRoots(c, nil, t.TempDir(), []string{filepath.Join(t.TempDir(), lock.FileName)})
	if out.CodeOf(err) != "lock-not-found" {
		t.Fatalf("missing named lock: %v", err)
	}
}

func TestCacheRootsCarryAnUnreadableNamedLock(t *testing.T) {
	p, _ := withSodium(t)
	p.save()
	broken := filepath.Join(t.TempDir(), lock.FileName)
	writeFile(t, broken, "{ not a lock")

	r := mustRoots(t, p.b.Cache, nil, p.b.Dir, broken)
	if r.Count() != 2 || !r.Project || r.LockFiles != 1 || len(r.Unreadable) != 1 || len(r.Locks) != 1 {
		t.Fatalf("roots: %+v", r)
	}
	_, err := r.Prune(p.b.Cache, cache.PruneOptions{})
	if out.CodeOf(err) != "cache-root-unreadable" {
		t.Fatalf("a prune refuses an unreadable named lock: %v", err)
	}
}

func TestBuildIngestsFilesBeforeSweepingThem(t *testing.T) {
	p := inPlaceProject(t)
	p.override("config/mine.txt", "settings worth keeping")
	p.mustBuild("client", Options{})
	if p.project("config/mine.txt") != "settings worth keeping" {
		t.Fatal("the override should have been placed")
	}

	if err := os.Remove(filepath.Join(p.b.Dir, "overrides", "config", "mine.txt")); err != nil {
		t.Fatal(err)
	}
	p.mustBuild("client", Options{})
	if exists(filepath.Join(p.b.Dir, "config", "mine.txt")) {
		t.Fatal("a file no longer in the source should be swept")
	}
	sum := sha512.Sum512([]byte("settings worth keeping"))
	if !p.b.Cache.Has(hex.EncodeToString(sum[:])) {
		t.Fatal("the bytes should reach the cache before the file is removed")
	}
}

func TestBuildFetchesAChangedCacheObjectAgain(t *testing.T) {
	p := newProject(t)
	sodium := p.modrinth.Publish(provider.Project{ID: "AANobbMI", Slug: "sodium", Title: "Sodium"}, provider.Version{ID: "QANobbMI", Number: "0.9.2", File: provider.File{Filename: "sodium-fabric-0.9.2+mc26.2.jar"}}, modJar(t, "sodium", "0.9.2"))
	p.lockMod("sodium", p.modrinth, sodium)
	p.save()
	if err := os.WriteFile(p.b.Cache.Object(sodium.File.Sha512), []byte("infected"), 0o600); err != nil {
		t.Fatal(err)
	}

	report := p.mustBuild("client", Options{})
	if got := p.built("client", "mods/"+sodium.File.Filename); got != string(p.cdn.Bytes(sodium)) {
		t.Fatal("the build places the locked bytes, fetched again")
	}
	if w := report.SecurityWarnings(WarnContext{}); len(w) != 1 || w[0].Protection != "cache-hash" || w[0].Message != "The cache held a changed copy of "+sodium.File.Filename+", so it was downloaded again." {
		t.Fatalf("security warnings: %+v", w)
	}
	if data, _ := os.ReadFile(p.b.Cache.Object(sodium.File.Sha512)); string(data) != string(p.cdn.Bytes(sodium)) {
		t.Fatal("the cache holds the locked bytes again")
	}
}

func TestBuildFailsOnAChangedCacheObjectWithNoURL(t *testing.T) {
	p := newProject(t)
	private := modJar(t, "private", "1.0")
	p.lockLocalMod("private", "private-1.0.jar", private)
	p.save()
	if err := os.WriteFile(p.b.Cache.Object(p.b.Lock.Mods["private"].Sha512), []byte("infected"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, err := p.build("client", Options{})
	if out.CodeOf(err) != "cache-changed" || !strings.Contains(err.Error(), "private-1.0.jar") {
		t.Fatalf("want cache-changed naming the file, got %v", err)
	}
	if p.hasBuilt("client", "mods/private-1.0.jar") {
		t.Fatal("a changed copy must not be placed")
	}
	entries, _ := os.ReadDir(p.builtPath("client", "mods"))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("a temp file was left: %s", e.Name())
		}
	}
}
