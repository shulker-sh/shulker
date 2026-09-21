package saves

import (
	"os"
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/saves/savestest"
)

func TestInUseOnlyWhileAGameHoldsTheLock(t *testing.T) {
	dir := t.TempDir()
	world(t, dir, "fresh")
	world(t, dir, "crashed")
	world(t, dir, "open")
	if err := os.WriteFile(filepath.Join(dir, "crashed", "session.lock"), []byte("☃"), 0o644); err != nil {
		t.Fatal(err)
	}
	savestest.Hold(t, filepath.Join(dir, "open"))

	for name, want := range map[string]bool{"fresh": false, "crashed": false, "open": true} {
		got, err := InUse(filepath.Join(dir, name))
		if err != nil || got != want {
			t.Errorf("InUse(%s) = %v, %v; want %v", name, got, err, want)
		}
	}
}
