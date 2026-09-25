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
	h.mustRun(t, "create", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "add", "sodium")
	if err := os.MkdirAll(filepath.Dir(h.config), 0o755); err != nil {
		t.Fatal(err)
	}
	elsewhere := `{"$schema":"https://shulker.sh/schema/v1/config.json","registry":"shared/instances.json"}`
	if err := os.WriteFile(h.config, []byte(elsewhere), 0o600); err != nil {
		t.Fatal(err)
	}

	prismDir := t.TempDir()
	h.mustRun(t, "link", "prism", h.dir, "--launcher-dir", prismDir, "--name", "Friends")
	instances, err := config.LoadInstances(filepath.Join(filepath.Dir(h.config), "shared", "instances.json"))
	if err != nil || len(instances) != 1 || instances[0].ID != "friends" {
		t.Fatalf("instances go to the registry path: %+v %v", instances, err)
	}
	if _, err := os.Stat(registryPath(h)); !os.IsNotExist(err) {
		t.Fatalf("the default registry must stay untouched: %v", err)
	}

	code, stdout, _ := h.run(t, "sync", "-C", t.TempDir(), "--json")
	if e := failureCode(t, stdout); code == 0 || len(e.Candidates) != 1 || !strings.HasPrefix(e.Candidates[0], "friends") {
		t.Fatalf("the candidates come from the registry the key names: %d %s", code, stdout)
	}
}
