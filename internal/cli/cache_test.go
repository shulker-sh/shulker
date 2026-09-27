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
	if !strings.Contains(stdout, "Cache "+h.cache) || !strings.Contains(stdout, "Used by this project") || !strings.Contains(stdout, "object") {
		t.Fatalf("cache info: %s", stdout)
	}
	if !strings.Contains(stdout, "prunable") || !strings.Contains(stdout, "shulker cache prune") {
		t.Fatalf("info should say what prune would free and how: %s", stdout)
	}

	stdout = h.mustRun(t, "cache", "prune")
	if !strings.Contains(stdout, "Freed") || !strings.Contains(stdout, " left") {
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
	if !strings.Contains(stdout, "Used by 1 lock file") {
		t.Fatalf("a named lock should count as a root: %s", stdout)
	}
	stdout = h.mustRun(t, "cache", "info", "--lock", named, "--lock", named)
	if !strings.Contains(stdout, "Used by this project and 2 lock files") {
		t.Fatalf("the flag repeats: %s", stdout)
	}
}
