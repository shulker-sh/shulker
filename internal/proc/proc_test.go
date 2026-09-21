package proc

import (
	"errors"
	"os/exec"
	"runtime"
	"testing"
)

func TestExitCodeIsAStatusNotAnError(t *testing.T) {
	if code, err := ExitCode(nil); code != 0 || err != nil {
		t.Fatalf("a clean exit: %d %v", code, err)
	}
	if runtime.GOOS == "windows" {
		t.Skip("needs sh")
	}
	code, err := ExitCode(exec.Command("sh", "-c", "exit 3").Run())
	if code != 3 || err != nil {
		t.Fatalf("a failed run: %d %v", code, err)
	}
	never := errors.New("no such program")
	if code, err := ExitCode(never); code != 0 || err != never {
		t.Fatalf("a program that never started: %d %v", code, err)
	}
}
