//go:build !windows

package pack

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGitDeadlineStopsItsHelpers(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\n/bin/sleep 30 &\n/bin/sleep 30\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err := (&Store{}).git(ctx, "fetch"); err == nil {
		t.Fatal("git should fail once its deadline passes")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("git returned %s after its deadline; a helper kept its output open", elapsed)
	}
}
