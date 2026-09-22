//go:build !windows

package proc

import (
	"os/exec"
	"testing"
)

func TestIsAliveTellsARunningProcessFromOneThatHasGone(t *testing.T) {
	cmd := exec.Command("sleep", "1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	if !IsAlive(cmd.Process.Pid) {
		t.Fatal("a process that has not exited is alive")
	}
	if err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
	if IsAlive(cmd.Process.Pid) {
		t.Fatal("a process that has exited and been reaped is not")
	}
	if IsAlive(0) || IsAlive(-1) {
		t.Fatal("no process is no process")
	}
}
