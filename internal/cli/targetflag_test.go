package cli

import (
	"os"
	"path/filepath"
	"testing"

	"shulker.sh/shulker/internal/out"
)

func TestTargetFlagMatchesTheArgument(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	h.mustRun(t, "target", "add", "extra", "--side", "client")
	h.mustRun(t, "build", "--target", "extra")
	if _, err := os.Stat(filepath.Join(h.dir, "build", "extra")); err != nil {
		t.Fatalf("--target extra did not build that target: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.dir, "build", "client")); err == nil {
		t.Fatal("--target extra built every target")
	}
}

func TestTargetGivenTwiceIsUsage(t *testing.T) {
	h := newHarness(t)
	h.mustRun(t, "init", "--yes", "--loader", "fabric", "--name", "pack")
	for _, args := range [][]string{
		{"build", "client", "--target", "client", "--json"},
		{"diff", "client", "--target", "client", "--json"},
		{"serve", "client", "--target", "client", "--json"},
	} {
		code, stdout, _ := h.run(t, args...)
		if e := failureCode(t, stdout); code != out.ExitUsage || e.Code != "usage" {
			t.Fatalf("%s: code=%d %s", args[0], code, stdout)
		}
	}
}
