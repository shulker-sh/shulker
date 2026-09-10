package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	t.Setenv(PathEnv, path)

	cfg, err := Load()
	if err != nil || cfg.CurseForge.Key != "" {
		t.Fatalf("missing file: %+v %v", cfg, err)
	}

	os.WriteFile(path, []byte(`{"curseforge":{"key":"abc"}}`), 0o600)
	cfg, err = Load()
	if err != nil || cfg.CurseForge.Key != "abc" {
		t.Fatalf("load: %+v %v", cfg, err)
	}

	os.WriteFile(path, []byte(`{`), 0o600)
	if _, err := Load(); err == nil {
		t.Fatal("expected a parse error")
	}
}
