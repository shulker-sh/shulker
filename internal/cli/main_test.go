package cli

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/launcher"
	"shulker.sh/shulker/internal/proc"
	"shulker.sh/shulker/internal/saves/savestest"
)

// TestMain points the config, cache and data directories at a scratch directory, so a test that
// forgets its own never repairs or rewrites the instances registered on the machine running it. It
// hides the machine's processes too, so a launcher open on it changes no test's output.
func TestMain(m *testing.M) {
	if !savestest.Main() {
		return
	}
	dir, err := os.MkdirTemp("", "shulker-cli-test")
	if err != nil {
		panic(err)
	}
	launcher.Processes = func() ([]proc.Process, error) { return nil, errors.New("no process list in tests") }
	os.Unsetenv("GITHUB_ACTIONS")
	os.Setenv("SHULKER_CONFIG", filepath.Join(dir, "config.json"))
	os.Setenv("SHULKER_CACHE", filepath.Join(dir, "cache"))
	os.Setenv("SHULKER_DATA", filepath.Join(dir, "data"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
