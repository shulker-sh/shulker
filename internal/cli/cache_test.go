package cli

import (
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/cache"
	"shulker.sh/shulker/internal/config"
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

func TestCachePruneKeepsWhatRootsReference(t *testing.T) {
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

	stdout := h.mustRun(t, "cache", "prune")
	if !strings.Contains(stdout, "freed") || !strings.Contains(stdout, "installer log") {
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
}

// A history entry leaves mod files to the cache, so the objects its lock names
// are roots of their own: without them a rollback could not run offline.
func TestCachePruneKeepsHistoryEntries(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "install")
	h.mustRun(t, "remove", "sodium")
	h.mustRun(t, "build")

	sodium := (&cache.Cache{Dir: h.cache}).Object(h.jars["sodium"].sha512)
	h.mustRun(t, "cache", "prune")
	if _, err := os.Stat(sodium); err != nil {
		t.Fatalf("the entry taken before the remove still needs sodium: %v", err)
	}

	h.mustRun(t, "rollback")
	if _, err := os.Stat(filepath.Join(h.dir, "mods", h.jars["sodium"].filename)); err != nil {
		t.Fatalf("the rollback should put sodium back from the cache: %v", err)
	}
}

func TestCacheInfoNamesItsRoots(t *testing.T) {
	h := newInPlace(t)
	h.mustRun(t, "add", "sodium")
	h.mustRun(t, "install")

	stdout := h.mustRun(t, "cache", "info")
	if !strings.Contains(stdout, "Cache "+h.cache) || !strings.Contains(stdout, "1 root (this project)") {
		t.Fatalf("cache info: %s", stdout)
	}
	if !strings.Contains(stdout, "object") {
		t.Fatalf("cache info should count objects: %s", stdout)
	}
}

func TestCachePruneRefusesAnUnreadableRoot(t *testing.T) {
	h := newInPlace(t)
	broken := t.TempDir()
	writeFile(t, filepath.Join(broken, "shulker.lock"), "{ not a lock")
	registry := map[string]any{"links": []config.Link{{
		Launcher: "prism", Side: "client", Name: "broken", Dir: broken, Target: "client",
	}}}
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(filepath.Dir(h.config), "registry.json"), string(data))

	code, stdout, _ := h.run(t, "--json", "cache", "prune")
	e := failureCode(t, stdout)
	if code == 0 || e.Code != "cache-root-unreadable" {
		t.Fatalf("unreadable root: code=%d %+v", code, e)
	}
}

// A registered instance that was deleted can need nothing, so it is skipped
// rather than blocking every prune until it is unlinked.
func TestCachePruneSkipsAGoneInstance(t *testing.T) {
	h := newInPlace(t)
	registry := map[string]any{"links": []config.Link{{
		Launcher: "prism", Side: "client", Name: "gone", Dir: filepath.Join(t.TempDir(), "deleted"), Target: "client",
	}}}
	data, err := json.Marshal(registry)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(filepath.Dir(h.config), "registry.json"), string(data))

	if stdout := h.mustRun(t, "cache", "info"); !strings.Contains(stdout, "1 root (this project)") {
		t.Fatalf("a gone instance should not count as a root: %s", stdout)
	}
}

func TestBuildIngestsFilesBeforeSweepingThem(t *testing.T) {
	h := newInPlace(t)
	override := filepath.Join(h.dir, "overrides", "config", "mine.txt")
	if err := os.MkdirAll(filepath.Dir(override), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, override, "settings worth keeping")
	h.mustRun(t, "install")
	if readFile(t, filepath.Join(h.dir, "config", "mine.txt")) != "settings worth keeping" {
		t.Fatal("the override should have been placed")
	}

	if err := os.Remove(override); err != nil {
		t.Fatal(err)
	}
	h.mustRun(t, "build")
	if _, err := os.Stat(filepath.Join(h.dir, "config", "mine.txt")); !os.IsNotExist(err) {
		t.Fatalf("a file no longer in the source should be swept: %v", err)
	}
	sum := sha512.Sum512([]byte("settings worth keeping"))
	if !(&cache.Cache{Dir: h.cache}).Has(hex.EncodeToString(sum[:])) {
		t.Fatal("the bytes should reach the cache before the file is removed")
	}
}
