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

func TestGitGivesUpOnAStalledTransfer(t *testing.T) {
	bin := t.TempDir()
	script := "#!/bin/sh\necho \"$GIT_HTTP_LOW_SPEED_LIMIT $GIT_HTTP_LOW_SPEED_TIME\"\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin)
	out, err := (&Store{}).git(context.Background(), "fetch")
	if err != nil || string(out) != "1 60\n" {
		t.Fatalf("git env: %q, %v", out, err)
	}
	stalled := "fatal: unable to access 'http://127.0.0.1:8765/mp.git/': Operation too slow. Less than 1 bytes/sec transferred the last 60 seconds"
	if !gitNetworkError(stalled) {
		t.Fatal("a stalled transfer should read as a network failure")
	}
}
