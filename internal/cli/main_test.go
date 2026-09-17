package cli

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain points the config and cache at a scratch directory, so a test that forgets its own
// never repairs or rewrites the instances registered on the machine running it.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "shulker-cli-test")
	if err != nil {
		panic(err)
	}
	os.Setenv("SHULKER_CONFIG", filepath.Join(dir, "config.json"))
	os.Setenv("SHULKER_CACHE", filepath.Join(dir, "cache"))
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
