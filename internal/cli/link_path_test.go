package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestShulkerPathPrefersTheNameOnPath(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	link := filepath.Join(dir, "shulker")
	if err := os.Symlink(exe, link); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got, err := shulkerPath()
	if err != nil || got != link {
		t.Fatalf("shulkerPath() = %q, %v; want %q", got, err, link)
	}
}

func TestShulkerPathIgnoresAnotherShulker(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "shulker"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got, err := shulkerPath()
	if err != nil || got != exe {
		t.Fatalf("shulkerPath() = %q, %v; want %q", got, err, exe)
	}
}
