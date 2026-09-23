//go:build !windows

package game

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"shulker.sh/shulker/internal/instance"
)

func TestDumpSignalsTheGameAndReadsTheDumpItPrints(t *testing.T) {
	dir := t.TempDir()
	dump := filepath.Join(dir, "dump.txt")
	if err := os.WriteFile(dump, []byte(sampleDump), 0o644); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "run.log")
	out, err := os.Create(log)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	cmd := exec.Command("sh", "-c", `trap 'cat "$0"' QUIT; echo ready; while :; do sleep 0.05; done`, dump)
	cmd.Stdout = out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
	for deadline := time.Now().Add(2 * time.Second); ; {
		if data, _ := os.ReadFile(log); strings.Contains(string(data), "ready") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the fake game never started")
		}
		time.Sleep(10 * time.Millisecond)
	}

	got, err := Dump(instance.Launch{PID: cmd.Process.Pid, Log: log})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, `"Render thread" #30`) {
		t.Fatalf("the dump is the one the game printed, got:\n%s", got)
	}
}
