package cache

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/lock"
)

func newCache(t *testing.T) *Cache {
	t.Helper()
	return &Cache{Dir: t.TempDir()}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// object writes content as a cache object and returns its sha512.
func object(t *testing.T, c *Cache, content string) string {
	t.Helper()
	sum := sha512.Sum512([]byte(content))
	sha := hex.EncodeToString(sum[:])
	writeFile(t, c.Object(sha), content)
	return sha
}

func TestPruneKeepsWhatARootReferences(t *testing.T) {
	c := newCache(t)
	mod, pack, server := object(t, c, "mod"), object(t, c, "pack"), object(t, c, "server")
	client, serverJar, library := object(t, c, "loader client"), object(t, c, "loader server"), object(t, c, "library")
	stray := object(t, c, "nothing references me")
	install := c.ATLauncherInstall("fabric", "0.17.3")
	writeFile(t, filepath.Join(install, "libraries", "a.jar"), "a")
	oldInstall := c.ATLauncherInstall("fabric", "0.17.2")
	writeFile(t, filepath.Join(oldInstall, "libraries", "a.jar"), "a")
	log := filepath.Join(c.Dir, "logs", "installer-20260101-000000.log")
	writeFile(t, log, "installer said things")
	leftover := filepath.Join(c.Dir, "tmp", "partial")
	writeFile(t, leftover, "half a download")
	runtime := filepath.Join(c.Dir, "java", "java-runtime-epsilon", "bin", "java")
	writeFile(t, runtime, "java")
	key := filepath.Join(c.Dir, "curseforge-key.json")
	writeFile(t, key, "{}")

	l := lock.New()
	l.Mods["sodium"] = lock.Mod{Sha512: mod}
	l.ResourcePacks["fresh"] = lock.Pack{Sha512: pack}
	l.Server = &lock.Download{Sha512: server}
	l.Loader = lock.Loader{Type: "fabric", Version: "0.17.3", Client: &lock.Download{Sha512: client}, Server: &lock.ServerJar{Sha512: serverJar, Libraries: map[string]lock.Download{"asm": {Sha512: library}}}}
	pruned, err := c.Prune([]Root{{Lock: l}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if pruned.Files != 1 || pruned.Installs != 1 || pruned.Logs != 1 || pruned.Temp != 1 || pruned.Checkouts != 0 || pruned.Bytes == 0 {
		t.Fatalf("pruned: %+v", pruned)
	}
	for _, sha := range []string{mod, pack, server, client, serverJar, library} {
		if !exists(c.Object(sha)) {
			t.Fatalf("a referenced object must survive a prune: %s", sha[:8])
		}
	}
	for _, path := range []string{install, runtime, key} {
		if !exists(path) {
			t.Fatalf("%s must survive a prune", path)
		}
	}
	for _, path := range []string{c.Object(stray), oldInstall, log, leftover} {
		if exists(path) {
			t.Fatalf("%s should be pruned", path)
		}
	}
}

func TestPruneDryRunRemovesNothing(t *testing.T) {
	c := newCache(t)
	stray := c.Object(object(t, c, "stray"))
	log := filepath.Join(c.Dir, "logs", "installer-20260101-000000.log")
	writeFile(t, log, "log")

	pruned, err := c.Prune(nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if pruned.Files != 1 || pruned.Logs != 1 || pruned.Empty() {
		t.Fatalf("a dry run should still count: %+v", pruned)
	}
	if !exists(stray) || !exists(log) {
		t.Fatal("a dry run should remove nothing")
	}
	if p := (Pruned{}); !p.Empty() {
		t.Fatal("nothing pruned should read as empty")
	}
}

// A root synced from a remote source keeps the source's mirror, its last-good records, and the
// checkouts those records point at, so an offline sync still finds the copy it falls back to.
func TestPruneKeepsASourcesMirrorAndFallbacks(t *testing.T) {
	c := newCache(t)
	root := Root{Source: "https://example.com/pack.git", Ref: "main", Path: "packs/one"}
	mirror := filepath.Join(c.PackMirror(root.Source), "HEAD")
	writeFile(t, mirror, "ref: refs/heads/main")
	writeFile(t, filepath.Join(c.PackMirror("https://example.com/other.git"), "HEAD"), "ref")
	record, _ := json.Marshal(map[string]string{"commit": "abc123", "sha256": "def456"})
	writeFile(t, c.LastGood(root.Source, root.Ref, root.Path), string(record))
	writeFile(t, c.LastGood("https://example.com/other.git", "", ""), string(record))
	kept := []string{
		filepath.Join(c.PackSource("abc123"), lock.FileName),
		c.PackManifest("def456"),
		filepath.Join(c.ProjectCheckout("def456"), lock.FileName),
	}
	dropped := []string{
		filepath.Join(c.PackSource("old111"), lock.FileName),
		c.PackManifest("old222"),
		c.PackLock("old222"),
		filepath.Join(c.ProjectCheckout("old333"), lock.FileName),
		filepath.Join(c.PackMirror("https://example.com/other.git"), "HEAD"),
		c.LastGood("https://example.com/other.git", "", ""),
	}
	for _, path := range append(slices.Clone(kept), dropped...) {
		writeFile(t, path, "x")
	}

	pruned, err := c.Prune([]Root{root}, false)
	if err != nil {
		t.Fatal(err)
	}
	if pruned.Checkouts != len(dropped) {
		t.Fatalf("pruned %d checkouts, want %d: %+v", pruned.Checkouts, len(dropped), pruned)
	}
	for _, path := range append(kept, mirror, c.LastGood(root.Source, root.Ref, root.Path)) {
		if !exists(path) {
			t.Fatalf("%s should survive", strings.TrimPrefix(path, c.Dir))
		}
	}
	for _, path := range dropped {
		if exists(path) {
			t.Fatalf("%s should be pruned", strings.TrimPrefix(path, c.Dir))
		}
	}
}

// A lock's modpacks keep their own sources and checkouts, and the files an archive laid as
// overrides.
func TestPruneKeepsALocksModpacks(t *testing.T) {
	c := newCache(t)
	archive, override := object(t, c, "archive"), object(t, c, "override")
	l := lock.New()
	l.Modpacks["base"] = lock.Modpack{
		Source: "https://example.com/base.git", Ref: "v1", Path: "", Commit: "c0ffee", Sha256: "5ea", LockSha256: "10c",
		Sha512: archive, Unmanaged: map[string]string{"config/x.txt": override},
	}
	kept := []string{
		filepath.Join(c.PackMirror("https://example.com/base.git"), "HEAD"),
		filepath.Join(c.PackSource("c0ffee"), lock.FileName),
		c.PackManifest("5ea"),
		filepath.Join(c.ProjectCheckout("5ea"), lock.FileName),
		c.PackLock("10c"),
		c.Object(archive),
		c.Object(override),
	}
	for _, path := range kept {
		writeFile(t, path, "x")
	}
	stray := filepath.Join(c.PackSource("dead"), lock.FileName)
	writeFile(t, stray, "x")

	if _, err := c.Prune([]Root{{Lock: l}}, false); err != nil {
		t.Fatal(err)
	}
	for _, path := range kept {
		if !exists(path) {
			t.Fatalf("%s should survive", strings.TrimPrefix(path, c.Dir))
		}
	}
	if exists(stray) {
		t.Fatal("an unreferenced checkout should be pruned")
	}
}

func TestSourceLocksNamesTheCheckoutsLocks(t *testing.T) {
	c := newCache(t)
	root := Root{Source: "https://example.com/pack.git", Ref: "main", Path: "packs/one"}
	record, _ := json.Marshal(map[string]string{"commit": "fallback", "sha256": ""})
	writeFile(t, c.LastGood(root.Source, root.Ref, root.Path), string(record))
	unrefd, _ := json.Marshal(map[string]string{"commit": "", "sha256": "unrefd"})
	writeFile(t, c.LastGood(root.Source, "", root.Path), string(unrefd))

	got := c.SourceLocks(root, "built", "built")
	want := []string{
		filepath.Join(c.PackSource("built"), "packs", "one", lock.FileName),
		filepath.Join(c.ProjectCheckout("built"), lock.FileName),
		filepath.Join(c.PackSource("fallback"), "packs", "one", lock.FileName),
		filepath.Join(c.ProjectCheckout("unrefd"), lock.FileName),
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("source locks:\n got %v\nwant %v", got, want)
	}
	if got := c.SourceLocks(Root{Source: "https://example.com/none.git"}, "", ""); len(got) != 0 {
		t.Fatalf("a source never built names no locks: %v", got)
	}
	if got := c.SourceLocks(root, "twice", ""); len(got) != 3 || got[0] != filepath.Join(c.PackSource("fallback"), "packs", "one", lock.FileName) {
		t.Fatalf("paths should be sorted and unique: %v", got)
	}
}

func TestUsageCountsObjects(t *testing.T) {
	c := newCache(t)
	object(t, c, "one")
	object(t, c, "two")
	writeFile(t, filepath.Join(c.Dir, "logs", "installer.log"), "1234")

	u, err := c.Usage()
	if err != nil {
		t.Fatal(err)
	}
	if u.Dir != c.Dir || u.Objects != 2 || u.Bytes != int64(len("one")+len("two")+4) {
		t.Fatalf("usage: %+v", u)
	}
	empty := newCache(t)
	if u, err := empty.Usage(); err != nil || u.Objects != 0 || u.Bytes != 0 {
		t.Fatalf("an empty cache: %+v %v", u, err)
	}
}

func TestIngestPutsAFilesBytesInTheCache(t *testing.T) {
	c := newCache(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "mine.txt")
	writeFile(t, file, "settings worth keeping")
	sum := sha512.Sum512([]byte("settings worth keeping"))
	sha := hex.EncodeToString(sum[:])

	if err := c.Ingest(file); err != nil {
		t.Fatal(err)
	}
	if !c.Has(sha) {
		t.Fatal("the file's bytes should be in the cache")
	}
	if err := c.Ingest(file); err != nil {
		t.Fatalf("ingesting again should be a no-op: %v", err)
	}
	if err := c.Ingest(filepath.Join(dir, "missing")); err != nil {
		t.Fatalf("a missing file is nothing to ingest: %v", err)
	}
	if err := c.Ingest(dir); err != nil {
		t.Fatalf("a directory is nothing to ingest: %v", err)
	}
	if u, _ := c.Usage(); u.Objects != 1 {
		t.Fatalf("only the file should be an object: %+v", u)
	}
}
