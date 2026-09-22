package build

import (
	"os"
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/lock"
	"shulker.sh/shulker/internal/manifest"
	"shulker.sh/shulker/internal/out"
)

func TestServerLevelNameReportsAnUnreadableLockItNeeds(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, lock.FileName), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := &manifest.Manifest{Name: "pack", Server: &manifest.Server{Properties: map[string]any{"level-name": "world-${minecraft.version}"}}}
	if _, err := serverLevelName(dir, m); out.CodeOf(err) != "lock-invalid" {
		t.Fatalf("a level-name that needs the lock names why it can't be read: %v", err)
	}

	m.Server.Properties["level-name"] = "world"
	if level, err := serverLevelName(dir, m); err != nil || level != "world" {
		t.Fatalf("a level-name that doesn't need the lock ignores it: %q %v", level, err)
	}
}
