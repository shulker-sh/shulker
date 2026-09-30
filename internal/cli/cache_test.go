package cli

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/build"
	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/config"
	"shulker.sh/shulker/internal/lock"
)

func strayObject(t *testing.T, c *cache.Cache, content string) string {
	t.Helper()
	sum := sha512.Sum512([]byte(content))
	sha := hex.EncodeToString(sum[:])
	path := c.Object(sha)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, content)
	return path
}

func TestCacheInfoNamesItsRootsAndPruneFreesTheRest(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "install")

	c := &cache.Cache{Dir: h.cache}
	sodium := c.Object(h.jars["sodium"].sha512)
	if _, err := os.Stat(sodium); err != nil {
		t.Fatalf("sodium should be in the cache after install: %v", err)
	}
	stray := strayObject(t, c, "nothing references me")
	log := filepath.Join(h.cache, "logs", "installer-20260101-000000.log")
	if err := os.MkdirAll(filepath.Dir(log), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, log, "installer said things")

	stdout := h.mustRun(t, "cache", "info")
	if !strings.Contains(stdout, "Cache "+h.cache) || !strings.Contains(stdout, "Kept for the project here") || !strings.Contains(stdout, " in 3 objects") || !strings.Contains(stdout, "Listing index: 0 pairs") {
		t.Fatalf("cache info: %s", stdout)
	}
	if !strings.Contains(stdout, "can be freed") || !strings.Contains(stdout, "shulker cache prune") {
		t.Fatalf("info should say what prune would free and how: %s", stdout)
	}
	if !strings.Contains(stdout, "Builds clone cached files into ") && !strings.Contains(stdout, "can't clone from the cache") {
		t.Fatalf("info should say whether builds clone: %s", stdout)
	}

	stdout = h.mustRun(t, "cache", "prune")
	if !strings.Contains(stdout, " of unused cache data") || !strings.Contains(stdout, " of cache data left") {
		t.Fatalf("prune output: %s", stdout)
	}
	if _, err := os.Stat(stray); !os.IsNotExist(err) {
		t.Fatalf("an unreferenced object should be pruned: %v", err)
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatalf("installer logs should be pruned: %v", err)
	}
	if _, err := os.Stat(sodium); err != nil {
		t.Fatalf("a locked mod must survive a prune: %v", err)
	}
	if stdout = h.mustRun(t, "cache", "prune"); !strings.Contains(stdout, "Nothing to prune") {
		t.Fatalf("a second prune has nothing to do: %s", stdout)
	}
}

func TestCachePruneKeepsManualDownloadsUnlessAsked(t *testing.T) {
	h := newInPlace(t)
	c := &cache.Cache{Dir: h.cache}
	sha, err := c.PutManual(strings.NewReader("a jar downloaded by hand"))
	if err != nil {
		t.Fatal(err)
	}
	if stdout := h.mustRun(t, "cache", "info"); !strings.Contains(stdout, "1 manual download, kept by prune") {
		t.Fatalf("info counts the manual download: %s", stdout)
	}
	h.mustRun(t, "cache", "prune")
	if !c.Has(sha) {
		t.Fatal("a plain prune keeps a manual download")
	}
	h.mustRun(t, "cache", "prune", "--manual")
	if c.Has(sha) {
		t.Fatal("prune --manual removes it")
	}
}

func TestCachePruneManualSaysWhichManualDownloadsItKept(t *testing.T) {
	h := newInPlace(t)
	c := &cache.Cache{Dir: h.cache}
	sha, err := c.PutManual(strings.NewReader("an imported pack"))
	if err != nil {
		t.Fatal(err)
	}
	l, err := lock.Load(filepath.Join(h.dir, lock.FileName))
	if err != nil {
		t.Fatal(err)
	}
	l.Imported = &lock.Imported{Sha512: sha}
	if err := l.Save(filepath.Join(h.dir, lock.FileName)); err != nil {
		t.Fatal(err)
	}

	stdout := h.mustRun(t, "cache", "prune", "--manual")

	if !c.Has(sha) || !strings.Contains(stdout, "Kept 1 manual download still in use; the cache is kept for the project here.") {
		t.Fatalf("prune --manual says why the used download stayed: %s", stdout)
	}
}

// A root that can't be read stops a prune, but an inspection still reports: it
// is the command that names which instance is broken.
func TestCacheInfoReportsAnUnreadableRoot(t *testing.T) {
	h := newInPlace(t)
	broken := t.TempDir()
	writeFile(t, filepath.Join(broken, "shulker.lock"), "{ not a lock")
	registry := map[string]any{"$schema": config.RegistrySchemaURL, "instances": []config.Instance{{
		ID: "broken", Launcher: "prism", Name: "broken", Dir: broken, Source: broken,
	}}}
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(filepath.Dir(h.config), "registry.json"), string(data))

	stdout, stderr := h.mustRunStderr(t, "cache", "info")
	if !strings.Contains(stderr, "can't be read") || !strings.Contains(stdout, "Cache ") {
		t.Fatalf("info should report and name the broken instance: stdout=%s stderr=%s", stdout, stderr)
	}
	if strings.Contains(stdout, "shulker cache prune") {
		t.Fatalf("a prune that would refuse should not be suggested: %s", stdout)
	}
	code, stdout, _ := h.run(t, "--json", "cache", "prune")
	if e := failureCode(t, stdout); code == 0 || e.Code != "cache-root-unreadable" {
		t.Fatalf("unreadable root: code=%d %+v", code, e)
	}
}

func TestCacheLockFlagCountsTheLockFile(t *testing.T) {
	h := newInPlace(t)
	named := filepath.Join(t.TempDir(), "other.lock")
	writeFile(t, named, readFile(t, filepath.Join(h.dir, "shulker.lock")))

	stdout := h.mustRun(t, "--dir", t.TempDir(), "cache", "info", "--lock", named)
	if !strings.Contains(stdout, "Kept for 1 lock file") {
		t.Fatalf("a named lock should count as a root: %s", stdout)
	}
	stdout = h.mustRun(t, "cache", "info", "--lock", named, "--lock", named)
	if !strings.Contains(stdout, "Kept for the project here and 2 lock files") {
		t.Fatalf("the flag repeats: %s", stdout)
	}
}

func cacheCheckOf(t *testing.T, stdout string) (bool, build.CacheCheck) {
	t.Helper()
	var env struct {
		OK   bool             `json:"ok"`
		Data build.CacheCheck `json:"data"`
	}
	if err := json.Unmarshal([]byte(stdout), &env); err != nil {
		t.Fatalf("%v: %s", err, stdout)
	}
	return env.OK, env.Data
}

func TestCacheVerifyFindsChangedObjectsAndFilesGoneFromAHistoryEntrysLock(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "build")
	c := &cache.Cache{Dir: h.cache}
	stray := strayObject(t, c, "nothing references me")
	if stdout := h.mustRun(t, "cache", "verify"); !strings.Contains(stdout, "No problems found (rehashed") || !strings.Contains(stdout, "$ shulker cache prune") {
		t.Fatalf("a clean cache: %s", stdout)
	}

	h.mustRun(t, "remove", "sodium")
	h.mustRun(t, "build")
	delete(h.jars, "sodium")
	code, stdout, _ := h.run(t, "cache", "verify", "--json")
	ok, v := cacheCheckOf(t, stdout)
	if code == 0 || ok || failureCode(t, stdout).Code != "cache-verify-failed" || len(v.Takedowns) != 1 || v.Takedowns[0].Keys[0] != "sodium" ||
		len(v.Takedowns[0].Roots) != 1 || !strings.Contains(v.Takedowns[0].Roots[0], "(history ") {
		t.Fatalf("sodium is gone, locked only by a history entry: exit %d %s", code, stdout)
	}
	if len(v.Unused) != 1 || c.Object(v.Unused[0].Sha512) != stray {
		t.Fatalf("unused: %+v", v.Unused)
	}

	writeFile(t, stray, "changed")
	code, stdout, stderr := h.run(t, "cache", "verify")
	if code == 0 || !strings.Contains(stdout, "1 object in the cache no longer matches its hash") || !strings.Contains(stderr, "cache-verify-failed") {
		t.Fatalf("a changed object fails the check: exit %d\n%s\n%s", code, stdout, stderr)
	}
	code, stdout, _ = h.run(t, "cache", "verify", "--fix", "--json")
	ok, v = cacheCheckOf(t, stdout)
	if _, err := os.Stat(stray); !v.Dropped || len(v.Changed) != 1 || err == nil {
		t.Fatalf("--fix drops the changed object: exit %d %s", code, stdout)
	}
}
