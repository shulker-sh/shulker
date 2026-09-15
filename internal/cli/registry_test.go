package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"shulker.sh/shulker/internal/config"
)

func TestRegistryFollowsTheConfigRegistryPath(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	if err := os.MkdirAll(filepath.Dir(h.config), 0o755); err != nil {
		t.Fatal(err)
	}
	oldLinks := `{"registry":"shared/instances.json","links":[{"side":"client","name":"old","dir":"/old","source":"/old","target":"client"}]}`
	if err := os.WriteFile(h.config, []byte(oldLinks), 0o600); err != nil {
		t.Fatal(err)
	}

	into := filepath.Join(t.TempDir(), "instance")
	h.mustRun(t, "sync", h.dir, "--into", into)
	links, err := config.LoadLinks(filepath.Join(filepath.Dir(h.config), "shared", "instances.json"))
	if err != nil || len(links) != 1 || links[0].Dir != into {
		t.Fatalf("entries go to the registry path: %+v %v", links, err)
	}
	if _, err := os.Stat(registryPath(h)); !os.IsNotExist(err) {
		t.Fatalf("the default registry must stay untouched: %v", err)
	}

	code, stdout, _ := h.run(t, "sync", "-C", t.TempDir(), "--json")
	if e := failureCode(t, stdout); code == 0 || len(e.Candidates) != 1 || strings.HasPrefix(e.Candidates[0], "old (") {
		t.Fatalf("a links key left in config.json is ignored: %d %s", code, stdout)
	}
}
